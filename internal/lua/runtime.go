package lua

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/rs/zerolog/log"
	lua "github.com/yuin/gopher-lua"

	"github.com/dokzlo13/lightd/internal/actions"
	"github.com/dokzlo13/lightd/internal/lua/modules"
	"github.com/dokzlo13/lightd/internal/lua/modules/collect"
)

// ErrRuntimeClosed is returned when the Lua runtime is closed
var ErrRuntimeClosed = fmt.Errorf("lua runtime closed")

// LuaWork represents work to be executed on the Lua VM
// All Lua execution MUST go through this to ensure thread safety
type LuaWork func(ctx context.Context)

// Runtime manages the Lua VM with single-threaded execution
type Runtime struct {
	L    *lua.LState
	deps RuntimeDeps

	// Modules
	actionModule  *modules.ActionModule
	schedModule   *modules.SchedModule
	hueModule     *modules.HueModule
	kvModule      *modules.KVModule
	sseModule     *modules.SSEModule
	webhookModule *modules.WebhookModule

	// Work queue for thread-safe Lua execution
	workQueue chan LuaWork

	// Shutdown signaling - closing this channel signals senders to stop
	// Using a channel in select is race-free (unlike mutex + bool)
	closing   chan struct{}
	closeOnce sync.Once
}

// NewRuntime creates a new Lua runtime
func NewRuntime(deps RuntimeDeps) *Runtime {
	L := lua.NewState()

	r := &Runtime{
		L:         L,
		deps:      deps,
		workQueue: make(chan LuaWork, 100),
		closing:   make(chan struct{}),
	}

	r.registerModules()

	return r
}

// Close signals the runtime to stop accepting new work and closes the Lua state.
// This is safe to call concurrently with Do/DoSync - they will see the closing signal.
func (r *Runtime) Close() {
	r.closeOnce.Do(func() {
		close(r.closing)
	})
	// Note: We don't close workQueue to avoid send-on-closed-channel panics.
	// The channel will be garbage collected when no longer referenced.
	// Run() will exit when it sees the closing signal.
	r.L.Close()
}

// Do queues work to be executed on the Lua VM (thread-safe, non-blocking)
// Returns false if the runtime is closing, queue is full, or context is cancelled.
// Uses channel-based signaling for race-free shutdown detection.
func (r *Runtime) Do(ctx context.Context, work LuaWork) bool {
	select {
	case <-r.closing:
		log.Warn().Msg("Lua runtime closing, dropping work")
		return false
	case <-ctx.Done():
		log.Warn().Msg("Context cancelled, dropping Lua work")
		return false
	case r.workQueue <- work:
		return true
	default:
		log.Warn().Msg("Lua work queue full, dropping work")
		return false
	}
}

// DoSync queues work and blocks until there's space (thread-safe, blocking)
// Returns error if the runtime is closing or context is cancelled.
// Uses channel-based signaling for race-free shutdown detection.
func (r *Runtime) DoSync(ctx context.Context, work LuaWork) error {
	select {
	case <-r.closing:
		return ErrRuntimeClosed
	case <-ctx.Done():
		return ctx.Err()
	case r.workQueue <- work:
		return nil
	}
}

// DoSyncWithResult queues work, waits for space, and waits for the result.
// This is used by the scheduler to invoke actions synchronously through the Lua worker.
// Uses channel-based signaling for race-free shutdown detection.
func (r *Runtime) DoSyncWithResult(ctx context.Context, work func(context.Context) error) error {
	done := make(chan error, 1)
	wrappedWork := LuaWork(func(c context.Context) {
		done <- work(c)
	})

	// Queue the work
	select {
	case <-r.closing:
		return ErrRuntimeClosed
	case <-ctx.Done():
		return ctx.Err()
	case r.workQueue <- wrappedWork:
		// Successfully queued
	}

	// Wait for result
	select {
	case <-r.closing:
		return ErrRuntimeClosed
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		return err
	}
}

// registerModules registers all Lua modules
func (r *Runtime) registerModules() {
	// Log module
	logModule := modules.NewLogModule()
	r.L.PreloadModule("log", logModule.Loader)

	// Geo module (uses shared calculator to avoid duplicate geocoding)
	geoCfg := r.deps.Config.Events.Scheduler.Geo
	geoModule := modules.NewGeoModule(geoCfg.Name, geoCfg.Timezone, r.deps.GeoCalc)
	r.L.PreloadModule("geo", geoModule.Loader)

	// Action module
	r.actionModule = modules.NewActionModule(r.deps.Registry, r.deps.Bridge, r.deps.Stores, r.deps.Orchestrator)
	r.L.PreloadModule("action", r.actionModule.Loader)

	// Sched module
	r.schedModule = modules.NewSchedModule(r.deps.Scheduler, r.deps.Config.Events.Scheduler.IsEnabled())
	r.L.PreloadModule("sched", r.schedModule.Loader)

	// Hue module
	r.hueModule = modules.NewHueModule(r.deps.Bridge, r.deps.SceneIndex)
	r.L.PreloadModule("hue", r.hueModule.Loader)

	// KV module (persistent key-value storage)
	r.kvModule = modules.NewKVModule(r.deps.KVManager)
	r.L.PreloadModule("kv", r.kvModule.Loader)

	// Collect module (event collectors for middleware)
	collectModule := collect.NewModule()
	r.L.PreloadModule("collect", collectModule.Loader)

	// Event source modules with dotted namespace
	// SSE module (Hue event stream events: button, rotary, connectivity)
	r.sseModule = modules.NewSSEModule(r.deps.Config.Events.SSE.IsEnabled())
	r.L.PreloadModule("events.sse", r.sseModule.Loader)

	// Webhook module (HTTP webhook events)
	r.webhookModule = modules.NewWebhookModule(r.deps.Config.Events.Webhook.Enabled)
	r.L.PreloadModule("events.webhook", r.webhookModule.Loader)
}

// Run starts the Lua worker goroutine - this is the ONLY goroutine that touches Lua
// It includes panic recovery to prevent crashes from killing the worker.
// Exits when context is cancelled or runtime is closed.
func (r *Runtime) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			r.drainQueue(ctx)
			return
		case <-r.closing:
			r.drainQueue(ctx)
			return
		case work := <-r.workQueue:
			r.executeWork(ctx, work)
		}
	}
}

// drainQueue processes any remaining work in the queue before exiting
func (r *Runtime) drainQueue(ctx context.Context) {
	for {
		select {
		case work := <-r.workQueue:
			r.executeWork(ctx, work)
		default:
			return
		}
	}
}

// executeWork runs a single work item with panic recovery
func (r *Runtime) executeWork(ctx context.Context, work LuaWork) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Error().
				Interface("panic", rec).
				Msg("Lua work panicked - worker continuing")
		}
	}()
	// Set context on LState so modules can access it via L.Context()
	r.L.SetContext(ctx)
	work(ctx)
}

// LoadScript loads and executes a Lua script (must be called before Run)
func (r *Runtime) LoadScript(path string) error {
	// Resolve path relative to config file
	if !filepath.IsAbs(path) {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			configDir := filepath.Dir(r.deps.Config.Script)
			path = filepath.Join(configDir, path)
		}
	}

	log.Info().Str("path", path).Msg("Loading Lua script")

	if err := r.L.DoFile(path); err != nil {
		return fmt.Errorf("failed to execute Lua script: %w", err)
	}

	log.Info().Msg("Lua script loaded successfully")
	return nil
}

// GetSSEModule returns the SSE module for handler registration
func (r *Runtime) GetSSEModule() *modules.SSEModule {
	return r.sseModule
}

// GetWebhookModule returns the webhook module for handler registration
func (r *Runtime) GetWebhookModule() *modules.WebhookModule {
	return r.webhookModule
}

// Invoker returns the action invoker
func (r *Runtime) Invoker() *actions.Invoker {
	return r.deps.Invoker
}
