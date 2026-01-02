package webhook

import (
	"context"
	"sync"

	"github.com/rs/zerolog/log"

	"github.com/dokzlo13/lightd/internal/actions"
	"github.com/dokzlo13/lightd/internal/events"
	"github.com/dokzlo13/lightd/internal/events/middleware"
	luactx "github.com/dokzlo13/lightd/internal/lua/context"
	"github.com/dokzlo13/lightd/internal/lua/exec"
)

// HandlerRegistry provides handler lookup functions
type HandlerRegistry interface {
	FindHandler(method, path string) *MatchResult
}

// RegisterHandlers subscribes to webhook events on the event bus and dispatches to handlers
func RegisterHandlers(
	ctx context.Context,
	registry HandlerRegistry,
	bus *events.Bus,
	invoker *actions.Invoker,
	luaExec exec.Executor,
) {
	collectors := make(map[string]middleware.Collector)
	var mu sync.Mutex

	bus.Subscribe(events.EventTypeWebhook, func(event events.Event) {
		method, _ := event.Data["method"].(string)
		path, _ := event.Data["path"].(string)
		body, _ := event.Data["body"].(string)
		jsonData, _ := event.Data["json"].(map[string]interface{})
		headers, _ := event.Data["headers"].(map[string]interface{})
		eventID, _ := event.Data["event_id"].(string)

		match := registry.FindHandler(method, path)
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

		// Build collector key from method and registered path pattern
		collectorKey := match.Handler.Method + ":" + match.Handler.Path

		mu.Lock()
		collector, ok := collectors[collectorKey]
		if !ok {
			collector = createWebhookCollector(ctx, match.Handler, invoker, luaExec)
			collectors[collectorKey] = collector
		}
		mu.Unlock()

		// Convert headers to map[string]any for collector
		headersAny := make(map[string]any)
		for k, v := range headers {
			headersAny[k] = v
		}

		// Convert json to map[string]any for collector
		jsonAny := make(map[string]any)
		for k, v := range jsonData {
			jsonAny[k] = v
		}

		collector.AddEvent(map[string]any{
			"method":      method,
			"path":        path,
			"body":        body,
			"json":        jsonAny,
			"headers":     headersAny,
			"path_params": match.PathParams,
			"event_id":    eventID,
		})
	})
}

// createWebhookCollector creates a collector for webhook events
func createWebhookCollector(
	ctx context.Context,
	handler *Handler,
	invoker *actions.Invoker,
	luaExec exec.Executor,
) middleware.Collector {
	onFlush := func(events []map[string]any) {
		luaExec.Do(ctx, func(workCtx context.Context) {
			var args map[string]any

			if handler.CollectorFactory != nil && handler.CollectorFactory.Reducer != nil {
				// Safe to call LState() here - we're inside Do() callback on Lua worker
				args = exec.CallReducer(luaExec.LState(), handler.CollectorFactory.Reducer, events)
			} else if len(events) > 0 {
				args = events[0]
			} else {
				args = make(map[string]any)
			}

			// Merge with static action args
			for k, v := range handler.ActionArgs {
				args[k] = v
			}

			// Extract request data for context
			method, _ := args["method"].(string)
			path, _ := args["path"].(string)
			body, _ := args["body"].(string)
			jsonData, _ := args["json"].(map[string]any)
			headers, _ := args["headers"].(map[string]any)
			pathParams, _ := args["path_params"].(map[string]string)
			eventID, _ := args["event_id"].(string)

			// Remove event metadata from args
			delete(args, "method")
			delete(args, "path")
			delete(args, "body")
			delete(args, "json")
			delete(args, "headers")
			delete(args, "path_params")
			delete(args, "event_id")

			// Convert headers back to map[string]interface{}
			headersIface := make(map[string]interface{})
			for k, v := range headers {
				headersIface[k] = v
			}

			// Convert json back to map[string]interface{}
			jsonIface := make(map[string]interface{})
			for k, v := range jsonData {
				jsonIface[k] = v
			}

			// Create request data to pass through context
			requestData := &luactx.RequestData{
				Method:     method,
				Path:       path,
				Body:       body,
				JSON:       jsonIface,
				Headers:    headersIface,
				PathParams: pathParams,
			}

			// Inject request data into context for the RequestModule to extract
			ctxWithRequest := context.WithValue(workCtx, luactx.RequestContextKey, requestData)
			if err := invoker.Invoke(ctxWithRequest, handler.ActionName, args, eventID); err != nil {
				log.Error().Err(err).Str("action", handler.ActionName).Msg("Failed to invoke webhook action")
			}
		})
	}

	if handler.CollectorFactory != nil {
		return handler.CollectorFactory.Create(onFlush)
	}
	return middleware.NewImmediateCollector(onFlush)
}
