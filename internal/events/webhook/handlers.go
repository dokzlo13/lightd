package webhook

import (
	"context"

	"github.com/rs/zerolog/log"

	"github.com/dokzlo13/lightd/internal/actions"
	"github.com/dokzlo13/lightd/internal/eventbus"
	luactx "github.com/dokzlo13/lightd/internal/lua/context"
)

// LuaExecutor provides thread-safe Lua execution
type LuaExecutor interface {
	Do(ctx context.Context, work func(ctx context.Context)) bool
}

// RegisterHandlers subscribes to webhook events on the event bus and dispatches to handlers
func (m *Module) RegisterHandlers(
	ctx context.Context,
	bus *eventbus.Bus,
	invoker *actions.Invoker,
	luaExec LuaExecutor,
) {
	bus.Subscribe(eventbus.EventTypeWebhook, func(event eventbus.Event) {
		method, _ := event.Data["method"].(string)
		path, _ := event.Data["path"].(string)
		body, _ := event.Data["body"].(string)
		jsonData, _ := event.Data["json"].(map[string]interface{})
		headers, _ := event.Data["headers"].(map[string]interface{})
		eventID, _ := event.Data["event_id"].(string)

		match := m.FindHandler(method, path)
		if match == nil {
			log.Debug().
				Str("method", method).
				Str("path", path).
				Msg("No webhook handler found for request")
			return
		}

		log.Debug().
			Str("method", method).
			Str("path", path).
			Str("handler_action", match.Handler.ActionName).
			Interface("path_params", match.PathParams).
			Msg("Webhook event matched handler")

		// Capture values for closure
		h := match.Handler
		pathParams := match.PathParams
		eid := eventID

		// Create request data to pass through context
		requestData := &luactx.RequestData{
			Method:     method,
			Path:       path,
			Body:       body,
			JSON:       jsonData,
			Headers:    headers,
			PathParams: pathParams,
		}

		// Queue work to Lua worker (single-threaded execution)
		luaExec.Do(ctx, func(workCtx context.Context) {
			// Inject request data into context for the RequestModule to extract
			ctxWithRequest := context.WithValue(workCtx, luactx.RequestContextKey, requestData)
			if err := invoker.Invoke(ctxWithRequest, h.ActionName, h.ActionArgs, eid); err != nil {
				log.Error().Err(err).Str("action", h.ActionName).Msg("Failed to invoke webhook action")
			}
		})
	})
}
