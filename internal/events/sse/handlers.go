package sse

import (
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/dokzlo13/lightd/internal/actions"
	"github.com/dokzlo13/lightd/internal/events"
	"github.com/dokzlo13/lightd/internal/events/middleware"
	"github.com/dokzlo13/lightd/internal/lua/exec"
	"github.com/dokzlo13/lightd/internal/lua/modules/collect"
	"github.com/google/uuid"
	"sync"
	"sync/atomic"
)

type HandlerRegistry interface {
	FindButtonHandler(string, string) *ButtonHandler
	FindConnectivityHandler(string, string) *ConnectivityHandler
	FindRotaryHandler(string) *RotaryHandler
	FindLightChangeHandlers(string, string) []*LightChangeHandler
}
type MutableRegistry interface {
	HandlerRegistry
	SetOnHandlersChanged(func())
}
type collectorCache struct {
	mu         sync.Mutex
	collectors map[string]middleware.Collector
	generation atomic.Uint64
}

func (c *collectorCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.generation.Add(1)
	for _, v := range c.collectors {
		v.Close()
	}
	c.collectors = map[string]middleware.Collector{}
}
func (c *collectorCache) add(key string, e map[string]any, create func(uint64) middleware.Collector) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	v := c.collectors[key]
	if v == nil {
		v = create(c.generation.Load())
		c.collectors[key] = v
	}
	return v.AddEvent(e)
}

// RegisterHandlers routes ordered admission without running Lua on source goroutines.
func RegisterHandlers(ctx context.Context, r HandlerRegistry, bus *events.Bus, i *actions.Invoker, x exec.Executor) {
	cache := &collectorCache{collectors: map[string]middleware.Collector{}}
	if mutable, ok := r.(MutableRegistry); ok {
		mutable.SetOnHandlersChanged(cache.Clear)
	}
	context.AfterFunc(ctx, cache.Clear)
	admit := func(kind events.EventType, key, action string, static map[string]any, factory *collect.CollectorFactory, e events.Event) error {
		data := copyEventData(e.Data)
		if _, ok := data["event_id"].(string); !ok {
			data["event_id"] = uuid.NewString()
		}
		return cache.add(string(kind)+":"+key, data, func(generation uint64) middleware.Collector {
			flush := func(batch []map[string]any) error {
				ids := ""
				for _, ev := range batch {
					ids += fmt.Sprint(ev["event_id"]) + "/"
				}
				dedupe := fmt.Sprintf("%s:%s:%x", kind, key, sha256.Sum256([]byte(ids)))
				return i.QueuePrepared(ctx, x, action, dedupe, string(kind), key, func(workCtx context.Context) (map[string]any, error) {
					if generation != cache.generation.Load() {
						return nil, fmt.Errorf("handler binding superseded")
					}
					var args map[string]any
					if factory != nil && factory.Reducer != nil {
						var err error
						args, err = exec.CallReducer(x.LState(), factory.Reducer, batch)
						if err != nil {
							return nil, err
						}
					} else {
						args = copyEventData(batch[0])
					}
					for k, v := range static {
						args[k] = v
					}
					delete(args, "event_id")
					if kind == events.EventTypeButton {
						delete(args, "resource_id")
						delete(args, "action")
					}
					return args, nil
				})
			}
			if factory != nil {
				return factory.Create(flush)
			}
			return middleware.NewImmediateCollector(flush)
		})
	}
	bus.SubscribeAck(events.EventTypeButton, func(e events.Event) error {
		id, _ := e.Data["resource_id"].(string)
		action, _ := e.Data["action"].(string)
		h := r.FindButtonHandler(id, action)
		if h == nil {
			return nil
		}
		return admit(e.Type, id+":"+action, h.ActionName, h.ActionArgs, h.CollectorFactory, e)
	})
	bus.SubscribeAck(events.EventTypeConnectivity, func(e events.Event) error {
		id, _ := e.Data["device_id"].(string)
		status, _ := e.Data["status"].(string)
		h := r.FindConnectivityHandler(id, status)
		if h == nil {
			return nil
		}
		return admit(e.Type, id+":"+status, h.ActionName, h.ActionArgs, h.CollectorFactory, e)
	})
	bus.SubscribeAck(events.EventTypeRotary, func(e events.Event) error {
		id, _ := e.Data["resource_id"].(string)
		h := r.FindRotaryHandler(id)
		if h == nil {
			return nil
		}
		return admit(e.Type, id, h.ActionName, h.ActionArgs, h.CollectorFactory, e)
	})
	bus.SubscribeAck(events.EventTypeLightChange, func(e events.Event) error {
		id, _ := e.Data["resource_id"].(string)
		typ, _ := e.Data["resource_type"].(string)
		for _, h := range r.FindLightChangeHandlers(id, typ) {
			key := h.ActionName + ":" + h.ResourceID.String() + ":" + h.ResourceType.String()
			if err := admit(e.Type, key, h.ActionName, h.ActionArgs, h.CollectorFactory, e); err != nil {
				return err
			}
		}
		return nil
	})
}
func copyEventData(data map[string]interface{}) map[string]any {
	out := make(map[string]any, len(data))
	for k, v := range data {
		out[k] = v
	}
	return out
}
