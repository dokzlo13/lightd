// Package webhook provides Lua bindings for HTTP webhook event handlers.
package webhook

import (
	"strings"

	"github.com/rs/zerolog/log"
	lua "github.com/yuin/gopher-lua"

	"github.com/dokzlo13/lightd/internal/lua/modules"
	"github.com/dokzlo13/lightd/internal/lua/modules/collect"
)

// Handler is called when a webhook event matches
type Handler struct {
	Method           string
	Path             string
	ActionName       string
	ActionArgs       map[string]any
	CollectorFactory *collect.CollectorFactory // nil = immediate
}

// Module provides events.webhook Lua module for webhook handlers
type Module struct {
	enabled  bool
	handlers []Handler
}

// NewModule creates a new webhook module
func NewModule(enabled bool) *Module {
	return &Module{
		enabled: enabled,
	}
}

// Loader is the module loader for Lua
func (m *Module) Loader(L *lua.LState) int {
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
func (m *Module) define(L *lua.LState) int {
	method := L.CheckString(1)
	path := L.CheckString(2)
	actionName := L.CheckString(3)
	argsTable := L.OptTable(4, L.NewTable())

	args := modules.LuaTableToMap(argsTable)

	// Extract collector factory from middleware field
	var factory *collect.CollectorFactory
	if mw := argsTable.RawGetString("middleware"); mw != lua.LNil {
		factory = collect.ExtractFactory(mw)
		delete(args, "middleware")
	}

	m.handlers = append(m.handlers, Handler{
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
func (m *Module) GetHandlers() []Handler {
	return m.handlers
}

// HasMatch checks if there's a registered handler for the given method and path.
// Implements the webhook.PathMatcher interface.
func (m *Module) HasMatch(method, path string) bool {
	return m.FindHandler(method, path) != nil
}

// MatchResult contains a matched handler and extracted path parameters
type MatchResult struct {
	Handler    *Handler
	PathParams map[string]string
}

// FindHandler finds a handler for a webhook event and extracts path parameters.
// Supports path patterns like "/group/{id}/toggle" where {id} is a parameter.
func (m *Module) FindHandler(method, path string) *MatchResult {
	for i := range m.handlers {
		h := &m.handlers[i]
		if h.Method != method {
			continue
		}

		params, ok := matchPath(h.Path, path)
		if ok {
			return &MatchResult{
				Handler:    h,
				PathParams: params,
			}
		}
	}
	return nil
}

// matchPath matches a path pattern against an actual path.
// Pattern: "/group/{id}/toggle"
// Path: "/group/123/toggle"
// Returns extracted params {"id": "123"} and true if matched.
func matchPath(pattern, path string) (map[string]string, bool) {
	patternParts := strings.Split(strings.Trim(pattern, "/"), "/")
	pathParts := strings.Split(strings.Trim(path, "/"), "/")

	// Must have same number of segments
	if len(patternParts) != len(pathParts) {
		return nil, false
	}

	params := make(map[string]string)

	for i, patternPart := range patternParts {
		pathPart := pathParts[i]

		// Check if this segment is a parameter (e.g., "{id}")
		if len(patternPart) > 2 && patternPart[0] == '{' && patternPart[len(patternPart)-1] == '}' {
			// Extract parameter name and value
			paramName := patternPart[1 : len(patternPart)-1]
			params[paramName] = pathPart
		} else if patternPart != pathPart {
			// Literal segment must match exactly
			return nil, false
		}
	}

	return params, true
}
