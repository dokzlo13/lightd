package lua

import (
	"context"
	"errors"
	"fmt"
	"github.com/dokzlo13/lightd/internal/actions"
	"github.com/dokzlo13/lightd/internal/lua/modules"
	"github.com/dokzlo13/lightd/internal/lua/modules/collect"
	"github.com/rs/zerolog/log"
	lua "github.com/yuin/gopher-lua"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var ErrRuntimeClosed = errors.New("lua runtime closed")
var ErrQueueFull = errors.New("lua work queue full")

type LuaWork func(context.Context)
type queuedWork struct {
	ctx  context.Context
	run  func(context.Context) error
	done chan error
}
type RuntimeStatus struct {
	Ready     bool   `json:"ready"`
	Queued    int    `json:"queued"`
	Running   bool   `json:"running"`
	LastError string `json:"last_error,omitempty"`
}
type Runtime struct {
	L             *lua.LState
	deps          RuntimeDeps
	actionModule  *modules.ActionModule
	schedModule   *modules.SchedModule
	hueModule     *modules.HueModule
	kvModule      *modules.KVModule
	sseModule     *modules.SSEModule
	webhookModule *modules.WebhookModule
	mu            sync.Mutex
	workQueue     chan queuedWork
	space         chan struct{}
	done          chan struct{}
	finishOnce    sync.Once
	closed        bool
	started       bool
	running       bool
	cancel        context.CancelFunc
	actionTimeout time.Duration
	lastError     string
}

func NewRuntime(deps RuntimeDeps) *Runtime {
	r := &Runtime{L: lua.NewState(), deps: deps, workQueue: make(chan queuedWork, 100), space: make(chan struct{}, 1), done: make(chan struct{}), actionTimeout: 30 * time.Second}
	r.registerModules()
	return r
}
func (r *Runtime) SetActionTimeout(d time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if d > 0 {
		r.actionTimeout = d
	}
}
func (r *Runtime) Status() RuntimeStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	return RuntimeStatus{Ready: r.started && !r.closed, Queued: len(r.workQueue), Running: r.running, LastError: r.lastError}
}
func (r *Runtime) Do(ctx context.Context, work LuaWork) bool {
	return r.enqueue(ctx, queuedWork{ctx: ctx, run: func(c context.Context) error { work(c); return nil }}, false) == nil
}
func (r *Runtime) DoSync(ctx context.Context, work LuaWork) error {
	return r.enqueue(ctx, queuedWork{ctx: ctx, run: func(c context.Context) error { work(c); return nil }}, true)
}
func (r *Runtime) DoSyncWithResult(ctx context.Context, work func(context.Context) error) error {
	item := queuedWork{ctx: ctx, run: work, done: make(chan error, 1)}
	if err := r.enqueue(ctx, item, true); err != nil {
		return err
	}
	select {
	case err := <-item.done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (r *Runtime) enqueue(ctx context.Context, item queuedWork, wait bool) error {
	for {
		r.mu.Lock()
		if r.closed {
			r.mu.Unlock()
			return ErrRuntimeClosed
		}
		if err := ctx.Err(); err != nil {
			r.mu.Unlock()
			return err
		}
		select {
		case r.workQueue <- item:
			r.mu.Unlock()
			return nil
		default:
			r.mu.Unlock()
		}
		if !wait {
			return ErrQueueFull
		}
		select {
		case <-r.space:
		case <-ctx.Done():
			return ctx.Err()
		case <-r.done:
			return ErrRuntimeClosed
		}
	}
}
func (r *Runtime) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := r.Stop(ctx); err != nil {
		log.Error().Err(err).Msg("Lua shutdown incomplete")
	}
}
func (r *Runtime) Stop(ctx context.Context) error {
	r.mu.Lock()
	if !r.closed {
		r.closed = true
		close(r.workQueue)
	}
	if r.cancel != nil {
		r.cancel()
	}
	started := r.started
	r.mu.Unlock()
	if !started {
		r.finish()
	}
	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (r *Runtime) finish() { r.finishOnce.Do(func() { r.L.Close(); close(r.done) }) }
func (r *Runtime) Run(parent context.Context) {
	r.mu.Lock()
	if r.started || r.closed {
		r.mu.Unlock()
		return
	}
	r.started = true
	ctx, cancel := context.WithCancel(parent)
	r.cancel = cancel
	r.mu.Unlock()
	defer cancel()
	defer r.finish()
	stop := context.AfterFunc(ctx, func() {
		r.mu.Lock()
		if !r.closed {
			r.closed = true
			close(r.workQueue)
		}
		r.mu.Unlock()
	})
	defer stop()
	for item := range r.workQueue {
		select {
		case r.space <- struct{}{}:
		default:
		}
		r.executeWork(ctx, item)
	}
}
func (r *Runtime) executeWork(runtimeCtx context.Context, item queuedWork) {
	r.mu.Lock()
	r.running = true
	timeout := r.actionTimeout
	r.mu.Unlock()
	ctx, cancel := context.WithTimeout(item.ctx, timeout)
	stop := context.AfterFunc(runtimeCtx, cancel)
	if runtimeCtx.Err() != nil {
		cancel()
	}
	defer cancel()
	defer stop()
	var err error
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("Lua work panic: %v", p)
		}
		r.mu.Lock()
		r.running = false
		if err != nil {
			r.lastError = err.Error()
		}
		r.mu.Unlock()
		if item.done != nil {
			item.done <- err
		}
		if err != nil {
			log.Error().Err(err).Msg("Lua work failed")
		}
	}()
	r.L.SetContext(ctx)
	// Invoke callbacks even when cancelled so accepted receipts receive a terminal outcome.
	err = item.run(ctx)
}

// registerModules registers all Lua modules
func (r *Runtime) registerModules() {
	// Log module
	logModule := modules.NewLogModule()
	r.L.PreloadModule("log", logModule.Loader)

	// Geo module (uses shared calculator, location already resolved at boot)
	geoModule := modules.NewGeoModule(r.deps.GeoCalc)
	r.L.PreloadModule("geo", geoModule.Loader)

	// Action module
	r.actionModule = modules.NewActionModule(r.deps.Registry, r.deps.Bridge, r.deps.Stores, r.deps.Orchestrator)
	r.actionModule.SetInvoker(r.deps.Invoker)
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

	// Utils module (sleep, etc.)
	utilsModule := modules.NewUtilsModule()
	r.L.PreloadModule("utils", utilsModule.Loader)

	// HTTP client module
	httpModule := modules.NewHTTPModule(&r.deps.Config.Clients.HTTP)
	r.L.PreloadModule("clients.http", httpModule.Loader)

	// Event source modules with dotted namespace
	// SSE module (Hue event stream events: button, rotary, connectivity)
	r.sseModule = modules.NewSSEModule(r.deps.Config.Events.SSE.IsEnabled())
	r.L.PreloadModule("events.sse", r.sseModule.Loader)

	// Webhook module (HTTP webhook events)
	r.webhookModule = modules.NewWebhookModule(r.deps.Config.Events.Webhook.Enabled)
	r.L.PreloadModule("events.webhook", r.webhookModule.Loader)
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

	ctx, cancel := context.WithTimeout(context.Background(), r.actionTimeout)
	defer cancel()
	r.L.SetContext(ctx)
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
