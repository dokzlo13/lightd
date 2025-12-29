package app

import (
	"context"

	"github.com/rs/zerolog/log"

	"github.com/dokzlo13/lightd/internal/actions"
	"github.com/dokzlo13/lightd/internal/config"
	"github.com/dokzlo13/lightd/internal/geo"
	"github.com/dokzlo13/lightd/internal/hue"
	luart "github.com/dokzlo13/lightd/internal/lua"
	"github.com/dokzlo13/lightd/internal/lua/modules"
	"github.com/dokzlo13/lightd/internal/reconcile"
	"github.com/dokzlo13/lightd/internal/scheduler"
	"github.com/dokzlo13/lightd/internal/state"
)

// LuaService wraps the Lua runtime and provides thread-safe execution.
type LuaService struct {
	cfg     *config.Config
	Runtime *luart.Runtime
	invoker *actions.Invoker
}

// NewLuaService creates a new LuaService.
func NewLuaService(
	cfg *config.Config,
	registry *actions.Registry,
	invoker *actions.Invoker,
	sched *scheduler.Scheduler,
	hueClient *hue.Client,
	groupCache *hue.GroupCache,
	desired *state.DesiredStore,
	reconciler *reconcile.Reconciler,
	geoCalc *geo.Calculator,
) (*LuaService, error) {
	runtime := luart.NewRuntime(cfg, registry, invoker, sched, hueClient, groupCache, desired, reconciler, geoCalc)

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

// Start begins the Lua worker goroutine and runs startup actions.
func (s *LuaService) Start(ctx context.Context) {
	// Start Lua worker goroutine - this is the ONLY goroutine that touches Lua
	go s.Runtime.Run(ctx)

	// Run startup actions immediately (all services are ready by now)
	go s.runStartupActions(ctx)
}

// runStartupActions runs initialization actions.
func (s *LuaService) runStartupActions(ctx context.Context) {
	log.Info().Msg("Running startup actions")
	s.Runtime.Do(ctx, func(workCtx context.Context) {
		if err := s.invoker.Invoke(workCtx, "ensure_banks_set", map[string]any{}, ""); err != nil {
			log.Error().Err(err).Msg("Failed to run ensure_banks_set")
		}
	})
}

// GetInputModule returns the input module for handler registration.
func (s *LuaService) GetInputModule() *modules.InputModule {
	return s.Runtime.GetInputModule()
}

// InvokeThroughLua invokes an action through the Lua worker for thread safety.
// This is used by the scheduler to ensure Lua actions run in the Lua worker goroutine.
func (s *LuaService) InvokeThroughLua(ctx context.Context, actionName string, args map[string]any, idempotencyKey, source, defID string) error {
	return s.Runtime.DoSyncWithResult(ctx, func(workCtx context.Context) error {
		return s.invoker.InvokeWithSource(workCtx, actionName, args, idempotencyKey, source, defID)
	})
}

// Do queues work to be executed on the Lua VM.
func (s *LuaService) Do(ctx context.Context, work luart.LuaWork) bool {
	return s.Runtime.Do(ctx, work)
}

// Close closes the Lua runtime.
func (s *LuaService) Close() {
	if s.Runtime != nil {
		s.Runtime.Close()
	}
}
