package app

import (
	"context"

	luastate "github.com/yuin/gopher-lua"

	"github.com/dokzlo13/lightd/internal/config"
	"github.com/dokzlo13/lightd/internal/events/sse"
	"github.com/dokzlo13/lightd/internal/events/webhook"
	"github.com/dokzlo13/lightd/internal/lua"
)

// LuaService wraps the Lua runtime and provides thread-safe execution.
type LuaService struct {
	cfg     *config.Config
	Runtime *lua.Runtime
}

// NewLuaService creates a new LuaService.
func NewLuaService(deps lua.RuntimeDeps) (*LuaService, error) {
	runtime := lua.NewRuntime(deps)

	return &LuaService{
		cfg:     deps.Config,
		Runtime: runtime,
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

// LState returns the underlying Lua state for module operations.
func (s *LuaService) LState() *luastate.LState {
	return s.Runtime.L
}
