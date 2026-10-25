package modules

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dokzlo13/lightd/internal/hue"
	"github.com/dokzlo13/lightd/internal/hue/bridge"
	lua "github.com/yuin/gopher-lua"
)

type HueModule struct{ bridge *bridge.Client }

func NewHueModule(b *bridge.Client, _ ...*hue.SceneIndex) *HueModule { return &HueModule{b} }
func (m *HueModule) Loader(L *lua.LState) int {
	mt := L.NewTypeMetatable("hue.resource")
	L.SetField(mt, "__index", L.SetFuncs(L.NewTable(), nativeHueMethods))
	mod := L.NewTable()
	L.SetField(mod, "group", L.NewFunction(m.factory("group")))
	L.SetField(mod, "light", L.NewFunction(m.factory("light")))
	L.SetField(mod, "groups", L.NewFunction(m.groups))
	L.SetField(mod, "lights", L.NewFunction(m.lights))
	L.SetField(mod, "scenes", L.NewFunction(m.scenes))
	L.SetField(mod, "refresh", L.NewFunction(func(L *lua.LState) int {
		if err := m.bridge.Refresh(hueContext(L)); err != nil {
			L.RaiseError("%s", err)
			return 0
		}
		L.Push(lua.LTrue)
		return 1
	}))
	L.Push(mod)
	return 1
}

type nativeHueResource struct {
	client   *bridge.Client
	kind, id string
}

func hueContext(L *lua.LState) context.Context {
	ctx := L.Context()
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
func pushNativeHue(L *lua.LState, b *bridge.Client, kind, id string) {
	u := L.NewUserData()
	u.Value = &nativeHueResource{b, kind, id}
	L.SetMetatable(u, L.GetTypeMetatable("hue.resource"))
	L.Push(u)
}
func checkNativeHue(L *lua.LState) (*nativeHueResource, *lua.LUserData) {
	u := L.CheckUserData(1)
	r, ok := u.Value.(*nativeHueResource)
	if !ok {
		L.ArgError(1, "Hue resource expected")
		return nil, nil
	}
	return r, u
}
func (m *HueModule) factory(kind string) lua.LGFunction {
	return func(L *lua.LState) int {
		id := L.CheckString(1)
		var err error
		if kind == "group" {
			_, err = m.bridge.Group(hueContext(L), id)
		} else {
			_, err = m.bridge.Light(hueContext(L), id)
		}
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
			return 2
		}
		pushNativeHue(L, m.bridge, kind, id)
		L.Push(lua.LNil)
		return 2
	}
}
func (m *HueModule) groups(L *lua.LState) int {
	t := L.NewTable()
	for i, g := range m.bridge.Groups() {
		pushNativeHue(L, m.bridge, "group", g.ID)
		t.RawSetInt(i+1, L.Get(-1))
		L.Pop(1)
	}
	L.Push(t)
	return 1
}
func (m *HueModule) lights(L *lua.LState) int {
	items, err := m.bridge.Lights(hueContext(L))
	if err != nil {
		L.RaiseError("%s", err)
		return 0
	}
	t := L.NewTable()
	for i, l := range items {
		pushNativeHue(L, m.bridge, "light", l.ID)
		t.RawSetInt(i+1, L.Get(-1))
		L.Pop(1)
	}
	L.Push(t)
	return 1
}
func (m *HueModule) scenes(L *lua.LState) int { pushHueJSON(L, m.bridge.Scenes()); return 1 }
func pushHueJSON(L *lua.LState, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		L.RaiseError("%s", err)
		return
	}
	var generic any
	if err = json.Unmarshal(data, &generic); err != nil {
		L.RaiseError("%s", err)
		return
	}
	L.Push(GoToLuaValue(L, generic))
}
func hueInput(L *lua.LState, index int, dst any) error {
	b, err := json.Marshal(LuaToGo(L.CheckTable(index)))
	if err != nil {
		return err
	}
	if err = json.Unmarshal(b, dst); err != nil {
		return fmt.Errorf("invalid Hue command: %w", err)
	}
	return nil
}
func hueApply(L *lua.LState, r *nativeHueResource, s bridge.State) int {
	var err error
	if r.kind == "group" {
		err = r.client.ApplyGroup(hueContext(L), r.id, s, "")
	} else {
		err = r.client.ApplyLight(hueContext(L), r.id, s)
	}
	if err != nil {
		L.RaiseError("%s", err)
		return 0
	}
	L.Push(L.Get(1))
	return 1
}
