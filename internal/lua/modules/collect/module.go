package collect

import (
	lua "github.com/yuin/gopher-lua"

	"github.com/dokzlo13/lightd/internal/events/middleware"
)

const collectorTypeName = "Collector"

// CollectorFactory holds config to create a collector later.
// This is needed because the flush callback is set by the handler, not at creation time in Lua.
type CollectorFactory struct {
	Type       string // "quiet", "count", "interval"
	QuietMs    int
	Count      int
	IntervalMs int
	Reducer    *lua.LFunction
}

// Create creates the actual Collector with the given flush callback
func (f *CollectorFactory) Create(onFlush middleware.FlushFunc) middleware.Collector {
	switch f.Type {
	case "quiet":
		return middleware.NewQuietCollector(f.QuietMs, onFlush)
	case "count":
		return middleware.NewCountCollector(f.Count, onFlush)
	case "interval":
		return middleware.NewIntervalCollector(f.IntervalMs, onFlush)
	default:
		return middleware.NewImmediateCollector(onFlush)
	}
}

// Module provides the collect Lua module
type Module struct{}

// NewModule creates a new collect module
func NewModule() *Module {
	return &Module{}
}

// Loader is the module loader for Lua
func (m *Module) Loader(L *lua.LState) int {
	// Register CollectorFactory type
	mt := L.NewTypeMetatable(collectorTypeName)
	L.SetGlobal(collectorTypeName, mt)

	mod := L.NewTable()
	L.SetField(mod, "quiet", L.NewFunction(m.quiet))
	L.SetField(mod, "count", L.NewFunction(m.count))
	L.SetField(mod, "interval", L.NewFunction(m.interval))

	L.Push(mod)
	return 1
}

// collect.quiet(ms, reducer) - Flush after ms of no new events
func (m *Module) quiet(L *lua.LState) int {
	ms := L.CheckInt(1)
	reducer := L.CheckFunction(2)

	factory := &CollectorFactory{
		Type:    "quiet",
		QuietMs: ms,
		Reducer: reducer,
	}

	ud := L.NewUserData()
	ud.Value = factory
	L.SetMetatable(ud, L.GetTypeMetatable(collectorTypeName))
	L.Push(ud)
	return 1
}

// collect.count(n, reducer) - Flush after n events
func (m *Module) count(L *lua.LState) int {
	n := L.CheckInt(1)
	reducer := L.CheckFunction(2)

	factory := &CollectorFactory{
		Type:  "count",
		Count: n,
		Reducer: reducer,
	}

	ud := L.NewUserData()
	ud.Value = factory
	L.SetMetatable(ud, L.GetTypeMetatable(collectorTypeName))
	L.Push(ud)
	return 1
}

// collect.interval(ms, reducer) - Flush every ms
func (m *Module) interval(L *lua.LState) int {
	ms := L.CheckInt(1)
	reducer := L.CheckFunction(2)

	factory := &CollectorFactory{
		Type:       "interval",
		IntervalMs: ms,
		Reducer:    reducer,
	}

	ud := L.NewUserData()
	ud.Value = factory
	L.SetMetatable(ud, L.GetTypeMetatable(collectorTypeName))
	L.Push(ud)
	return 1
}

// ExtractFactory extracts CollectorFactory from Lua userdata
func ExtractFactory(v lua.LValue) *CollectorFactory {
	if ud, ok := v.(*lua.LUserData); ok {
		if factory, ok := ud.Value.(*CollectorFactory); ok {
			return factory
		}
	}
	return nil
}

