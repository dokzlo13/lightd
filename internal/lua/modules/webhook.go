package modules

import (
	"github.com/rs/zerolog/log"
	glua "github.com/yuin/gopher-lua"

	"github.com/dokzlo13/lightd/internal/events/webhook"
	"github.com/dokzlo13/lightd/internal/lua/modules/collect"
)

// WebhookModule provides events.webhook Lua module for webhook handlers
type WebhookModule struct {
	enabled  bool
	handlers []webhook.Handler
}

// NewWebhookModule creates a new webhook module
func NewWebhookModule(enabled bool) *WebhookModule {
	return &WebhookModule{
		enabled: enabled,
	}
}

// Loader is the module loader for Lua
func (m *WebhookModule) Loader(L *glua.LState) int {
	if !m.enabled {
		L.RaiseError("events.webhook module is disabled (webhook.enabled: false in config)")
		return 0
	}

	mod := L.NewTable()

	L.SetField(mod, "define", L.NewFunction(m.define))

	L.Push(mod)
	return 1
}

// define(method, path, action_name, args) - Register a webhook handler
func (m *WebhookModule) define(L *glua.LState) int {
	method := L.CheckString(1)
	path := L.CheckString(2)
	actionName := L.CheckString(3)
	argsTable := L.OptTable(4, L.NewTable())

	args := LuaTableToMap(argsTable)

	// Extract collector factory from middleware field
	var factory *collect.CollectorFactory
	if mw := argsTable.RawGetString("middleware"); mw != glua.LNil {
		factory = collect.ExtractFactory(mw)
		delete(args, "middleware")
	}

	m.handlers = append(m.handlers, webhook.Handler{
		Method:           method,
		Path:             path,
		ActionName:       actionName,
		ActionArgs:       args,
		CollectorFactory: factory,
	})

	log.Info().
		Str("method", method).
		Str("path", path).
		Str("action", actionName).
		Msg("Registered webhook handler")

	return 0
}

// GetHandlers returns all registered webhook handlers
func (m *WebhookModule) GetHandlers() []webhook.Handler {
	return m.handlers
}

// HasMatch checks if there's a registered handler for the given method and path.
// Implements the webhook.PathMatcher interface.
func (m *WebhookModule) HasMatch(method, path string) bool {
	return m.FindHandler(method, path) != nil
}

// FindHandler finds a handler for a webhook event and extracts path parameters.
// Supports path patterns like "/group/{id}/toggle" where {id} is a parameter.
func (m *WebhookModule) FindHandler(method, path string) *webhook.MatchResult {
	for i := range m.handlers {
		h := &m.handlers[i]
		if h.Method != method {
			continue
		}

		params, ok := webhook.MatchPath(h.Path, path)
		if ok {
			return &webhook.MatchResult{
				Handler:    h,
				PathParams: params,
			}
		}
	}
	return nil
}
