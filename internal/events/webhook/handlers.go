package webhook

import (
	"context"
	"fmt"
	"github.com/dokzlo13/lightd/internal/actions"
	"github.com/dokzlo13/lightd/internal/events"
	"github.com/dokzlo13/lightd/internal/events/middleware"
	luactx "github.com/dokzlo13/lightd/internal/lua/context"
	"github.com/dokzlo13/lightd/internal/lua/exec"
	"sync"
)

type HandlerRegistry interface {
	FindHandler(string, string) *MatchResult
}

func RegisterHandlers(ctx context.Context, r HandlerRegistry, bus *events.Bus, i *actions.Invoker, x exec.Executor) {
	var mu sync.Mutex
	collectors := map[string]middleware.Collector{}
	context.AfterFunc(ctx, func() {
		mu.Lock()
		defer mu.Unlock()
		for _, c := range collectors {
			c.Close()
		}
	})
	bus.SubscribeAck(events.EventTypeWebhook, func(e events.Event) error {
		method, _ := e.Data["method"].(string)
		path, _ := e.Data["path"].(string)
		match := r.FindHandler(method, path)
		if match == nil {
			return fmt.Errorf("webhook handler not found")
		}
		data := map[string]any{}
		for k, v := range e.Data {
			data[k] = v
		}
		data["path_params"] = match.PathParams
		key := method + ":" + match.Handler.Path
		mu.Lock()
		defer mu.Unlock()
		collector := collectors[key]
		if collector == nil {
			collector = createWebhookCollector(ctx, match.Handler, i, x)
			collectors[key] = collector
		}
		return collector.AddEvent(data)
	})
}
func createWebhookCollector(ctx context.Context, h *Handler, i *actions.Invoker, x exec.Executor) middleware.Collector {
	flush := func(batch []map[string]any) error {
		first := batch[0]
		method, _ := first["method"].(string)
		path, _ := first["path"].(string)
		body, _ := first["body"].(string)
		data, _ := first["json"].(map[string]any)
		headers, _ := first["headers"].(map[string]any)
		params, _ := first["path_params"].(map[string]string)
		req := &luactx.RequestData{Method: method, Path: path, Body: body, JSON: data, Headers: headers, PathParams: params}
		workCtx := context.WithValue(ctx, luactx.RequestContextKey, req)
		key := ""
		for _, ev := range batch {
			key += fmt.Sprint(ev["event_id"]) + "/"
		}
		return i.QueuePrepared(workCtx, x, h.ActionName, key, "webhook", h.Method+":"+h.Path, func(context.Context) (map[string]any, error) {
			var args map[string]any
			if h.CollectorFactory != nil && h.CollectorFactory.Reducer != nil {
				var err error
				args, err = exec.CallReducer(x.LState(), h.CollectorFactory.Reducer, batch)
				if err != nil {
					return nil, err
				}
			} else {
				args = map[string]any{}
				for k, v := range first {
					args[k] = v
				}
			}
			for _, k := range []string{"method", "path", "body", "json", "headers", "path_params", "event_id"} {
				delete(args, k)
			}
			for k, v := range h.ActionArgs {
				args[k] = v
			}
			return args, nil
		})
	}
	if h.CollectorFactory != nil {
		return h.CollectorFactory.Create(flush)
	}
	return middleware.NewImmediateCollector(flush)
}
