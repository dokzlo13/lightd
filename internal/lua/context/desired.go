package context

import (
	"context"
	"database/sql"
	"encoding/json"
	lua "github.com/yuin/gopher-lua"
	"sort"

	"github.com/dokzlo13/lightd/internal/hue/reconcile/group"
	"github.com/dokzlo13/lightd/internal/hue/reconcile/light"
	"github.com/dokzlo13/lightd/internal/storage"
)

// DesiredModule provides ctx.desired for accessing/modifying desired state.
//
// Chainable builder API:
//
//	ctx.desired:group("1"):on():set_scene("Relax")
//	ctx.desired:light("5"):on():set_bri(254)
//	ctx:reconcile()  -- flushes pending and triggers reconciler
type DesiredModule struct {
	groupStore *storage.TypedStore[group.Desired]
	lightStore *storage.TypedStore[light.Desired]

	// Pending builders (keyed by ID)
	pendingGroups map[string]*GroupDesiredBuilder
	pendingLights map[string]*LightDesiredBuilder
	closed        bool
	actualGroup   *group.ActualProvider
	actualLight   *light.ActualProvider
}

// NewDesiredModule creates a new desired state module.
func NewDesiredModule(
	groupStore *storage.TypedStore[group.Desired],
	lightStore *storage.TypedStore[light.Desired],
) *DesiredModule {
	return &DesiredModule{
		groupStore:    groupStore,
		lightStore:    lightStore,
		pendingGroups: make(map[string]*GroupDesiredBuilder),
		pendingLights: make(map[string]*LightDesiredBuilder),
	}
}

// Name returns "desired" - the field name in ctx.
func (m *DesiredModule) Name() string {
	return "desired"
}

// Install adds ctx.desired to the context table.
func (m *DesiredModule) Install(L *lua.LState, ctx *lua.LTable) {
	// Register builder metatables
	RegisterGroupBuilderType(L)
	RegisterLightBuilderType(L)

	desired := L.NewTable()

	// Chainable builder factories
	L.SetField(desired, "group", L.NewFunction(m.getGroupBuilder()))
	L.SetField(desired, "light", L.NewFunction(m.getLightBuilder()))

	L.SetField(ctx, m.Name(), desired)
}

// markGroupPending marks a group builder as having pending changes.
func (m *DesiredModule) markGroupPending(builder *GroupDesiredBuilder) {
	m.pendingGroups[builder.groupID] = builder
}

// markLightPending marks a light builder as having pending changes.
func (m *DesiredModule) markLightPending(builder *LightDesiredBuilder) {
	m.pendingLights[builder.lightID] = builder
}

// FlushTx writes this invocation's pending resource patches in the caller's transaction.
func (m *DesiredModule) FlushTx(tx *sql.Tx) error {
	ids := make([]string, 0, len(m.pendingGroups))
	for id := range m.pendingGroups {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		b := m.pendingGroups[id]
		patch, err := desiredPatch(b.state, b.extra)
		if err != nil {
			return err
		}
		if err = m.groupStore.PatchTx(tx, id, patch, b.clear); err != nil {
			return err
		}
	}
	ids = ids[:0]
	for id := range m.pendingLights {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		b := m.pendingLights[id]
		patch, err := desiredPatch(b.state, b.extra)
		if err != nil {
			return err
		}
		if err = m.lightStore.PatchTx(tx, id, patch, b.clear); err != nil {
			return err
		}
	}
	return nil
}
func desiredPatch(state any, extra map[string]any) (map[string]json.RawMessage, error) {
	b, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}
	out := map[string]json.RawMessage{}
	if err = json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	for k, v := range extra {
		raw, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		out[k] = raw
	}
	return out, nil
}
func (m *DesiredModule) clearPending() {
	for _, b := range m.pendingGroups {
		b.state = group.Desired{}
		b.extra = map[string]any{}
		b.clear = nil
	}
	for _, b := range m.pendingLights {
		b.state = light.Desired{}
		b.extra = map[string]any{}
		b.clear = nil
	}
	m.pendingGroups = map[string]*GroupDesiredBuilder{}
	m.pendingLights = map[string]*LightDesiredBuilder{}
}

// Flush is an explicit checkpoint; later Lua failure cannot undo this commit.
func (m *DesiredModule) Flush() error {
	err := m.groupStore.Transaction(context.Background(), m.FlushTx)
	if err == nil {
		m.clearPending()
	}
	return err
}

// Close prevents builders captured in Lua globals escaping their invocation.
func (m *DesiredModule) Close() { m.closed = true }

// getGroupBuilder returns a Lua function that creates a group builder.
func (m *DesiredModule) getGroupBuilder() lua.LGFunction {
	return func(L *lua.LState) int {
		L.CheckTable(1) // self
		groupID := L.CheckString(2)

		pushGroupBuilder(L, groupID, m)
		return 1
	}
}

// getLightBuilder returns a Lua function that creates a light builder.
func (m *DesiredModule) getLightBuilder() lua.LGFunction {
	return func(L *lua.LState) int {
		L.CheckTable(1) // self
		lightID := L.CheckString(2)

		pushLightBuilder(L, lightID, m)
		return 1
	}
}

func (m *DesiredModule) SetActualProviders(g *group.ActualProvider, l *light.ActualProvider) {
	m.actualGroup = g
	m.actualLight = l
}
