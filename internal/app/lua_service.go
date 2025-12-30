package app

import (
	"context"

	"github.com/amimof/huego"

	"github.com/dokzlo13/lightd/internal/actions"
	"github.com/dokzlo13/lightd/internal/cache"
	"github.com/dokzlo13/lightd/internal/config"
	"github.com/dokzlo13/lightd/internal/events/sse"
	"github.com/dokzlo13/lightd/internal/events/webhook"
	"github.com/dokzlo13/lightd/internal/geo"
	"github.com/dokzlo13/lightd/internal/lua"
	"github.com/dokzlo13/lightd/internal/reconcile"
	"github.com/dokzlo13/lightd/internal/scheduler"
	"github.com/dokzlo13/lightd/internal/stores"
)

// LuaService wraps the Lua runtime and provides thread-safe execution.
type LuaService struct {
	cfg     *config.Config
	Runtime *lua.Runtime
	invoker *actions.Invoker
}

// NewLuaService creates a new LuaService.
func NewLuaService(
	cfg *config.Config,
	registry *actions.Registry,
	invoker *actions.Invoker,
	sched *scheduler.Scheduler,
	bridge *huego.Bridge,
	sceneIndex *cache.SceneIndex,
	storeRegistry *stores.Registry,
	orchestrator *reconcile.Orchestrator,
	geoCalc *geo.Calculator,
) (*LuaService, error) {
	runtime := lua.NewRuntime(cfg, registry, invoker, sched, bridge, sceneIndex, storeRegistry, orchestrator, geoCalc)

	return &LuaService{
		cfg:     cfg,
		Runtime: runtime,
		invoker: invoker,
	}, nil
}

// LoadScript loads and executes the Lua script.
// Must be called before Start().
func (s *LuaService) LoadScript() error {
	if err := s.Runtime.LoadScript(s.cfg.Script); err != nil {
		return err
	}
	return nil
}

// Start begins the Lua worker goroutine.
func (s *LuaService) Start(ctx context.Context) {
	// Start Lua worker goroutine - this is the ONLY goroutine that touches Lua
	go s.Runtime.Run(ctx)
}

// GetSSEModule returns the SSE module for handler registration.
func (s *LuaService) GetSSEModule() *sse.Module {
	return s.Runtime.GetSSEModule()
}

// GetWebhookModule returns the webhook module for handler registration.
func (s *LuaService) GetWebhookModule() *webhook.Module {
	return s.Runtime.GetWebhookModule()
}

// InvokeThroughLua invokes an action through the Lua worker for thread safety.
// This is used by the scheduler to ensure Lua actions run in the Lua worker goroutine.
func (s *LuaService) InvokeThroughLua(ctx context.Context, actionName string, args map[string]any, idempotencyKey, source, defID string) error {
	return s.Runtime.DoSyncWithResult(ctx, func(workCtx context.Context) error {
		return s.invoker.InvokeWithSource(workCtx, actionName, args, idempotencyKey, source, defID)
	})
}

// Do queues work to be executed on the Lua VM.
// This method satisfies the sse.LuaExecutor and webhook.LuaExecutor interfaces.
func (s *LuaService) Do(ctx context.Context, work func(ctx context.Context)) bool {
	return s.Runtime.Do(ctx, work)
}

// Close closes the Lua runtime.
func (s *LuaService) Close() {
	if s.Runtime != nil {
		s.Runtime.Close()
	}
}
