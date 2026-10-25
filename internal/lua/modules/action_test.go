package modules

import (
	"context"
	"github.com/dokzlo13/lightd/internal/actions"
	"github.com/dokzlo13/lightd/internal/hue"
	"github.com/dokzlo13/lightd/internal/storage"
	lua "github.com/yuin/gopher-lua"
	"path/filepath"
	"testing"
)

const fixtureGroup = "11111111-1111-4111-8111-111111111111"

func actionFixture(t *testing.T) (*lua.LState, *actions.Invoker, *hue.StoreRegistry, *storage.DB) {
	t.Helper()
	db, e := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	L := lua.NewState()
	t.Cleanup(L.Close)
	r := actions.NewRegistry()
	stores := hue.NewStoreRegistry(storage.NewStore(db.DB))
	i := actions.NewInvoker(r, storage.NewLedger(db.DB), func(ctx context.Context) *actions.Context { return actions.NewContext(ctx, nil, nil, nil, nil) })
	m := NewActionModule(r, nil, stores, nil)
	m.SetInvoker(i)
	L.PreloadModule("action", m.Loader)
	return L, i, stores, db
}
func TestDesiredFramesMergeRollbackAndNestedScope(t *testing.T) {
	L, i, stores, db := actionFixture(t)
	L.SetGlobal("group_id", lua.LString(fixtureGroup))
	L.SetGlobal("rows", L.NewFunction(func(L *lua.LState) int {
		var n int
		db.QueryRow(`SELECT count(*) FROM resource_state`).Scan(&n)
		L.Push(lua.LNumber(n))
		return 1
	}))
	script := `local a=require("action")
 a.define("child",function(ctx) assert(rows()==0) end)
 a.define("parent",function(ctx)
  ctx.desired:group(group_id):on()
  ctx.desired:group(group_id):set_brightness(42)
  a.run("child")
  assert(rows()==0)
 end)
 a.define("bad",function(ctx)ctx.desired:group(group_id):set_brightness(99);error("fixture")end)
 a.define("mode",function(ctx)ctx.desired:group(group_id):set_scene("Warm");ctx.desired:group(group_id):set_ct(300):preserve_power()end)
 `
	if e := L.DoString(script); e != nil {
		t.Fatal(e)
	}
	if e := i.Invoke(context.Background(), "parent", nil, ""); e != nil {
		t.Fatal(e)
	}
	d, _, e := stores.Groups().Get(fixtureGroup)
	if e != nil || d.On == nil || !*d.On || d.Brightness == nil || *d.Brightness != 42 {
		t.Fatalf("lost patch %+v %v", d, e)
	}
	if e = i.Invoke(context.Background(), "bad", nil, ""); e == nil {
		t.Fatal("failure expected")
	}
	d, _, _ = stores.Groups().Get(fixtureGroup)
	if *d.Brightness != 42 {
		t.Fatal("failed frame committed")
	}
	if e = i.Invoke(context.Background(), "mode", nil, ""); e != nil {
		t.Fatal(e)
	}
	d, _, _ = stores.Groups().Get(fixtureGroup)
	if d.SceneName != "" || d.Ct == nil || d.On != nil || d.PowerMode != "preserve" {
		t.Fatalf("mode/power conflict %+v", d)
	}
	var parents int
	db.QueryRow(`SELECT count(*) FROM invocations WHERE json_extract(metadata,'$.parent_id')!=''`).Scan(&parents)
	if parents != 1 {
		t.Fatalf("nested identity %d", parents)
	}
}
func TestExplicitCheckpointAndEscapedBuilder(t *testing.T) {
	L, i, stores, _ := actionFixture(t)
	L.SetGlobal("group_id", lua.LString(fixtureGroup))
	if e := L.DoString(`local a=require("action");a.define("checkpoint",function(ctx) saved=ctx.desired:group(group_id):set_brightness(42);ctx:reconcile();saved:set_brightness(99);error("fixture")end);a.define("escaped",function(ctx)saved:set_brightness(5)end)`); e != nil {
		t.Fatal(e)
	}
	if i.Invoke(context.Background(), "checkpoint", nil, "") == nil {
		t.Fatal("wanted failure")
	}
	d, _, _ := stores.Groups().Get(fixtureGroup)
	if d.Brightness == nil || *d.Brightness != 42 {
		t.Fatal("checkpoint lost")
	}
	if i.Invoke(context.Background(), "escaped", nil, "") == nil {
		t.Fatal("escaped builder reused")
	}
}

func TestMultipleBuilderHandlesShareFrameAcrossCheckpoint(t *testing.T) {
	L, i, stores, _ := actionFixture(t)
	L.SetGlobal("group_id", lua.LString(fixtureGroup))
	if e := L.DoString(`local a=require("action");a.define("alias",function(ctx)
 local first=ctx.desired:group(group_id)
 local second=ctx.desired:group(group_id)
 first:on();second:set_brightness(42);ctx:reconcile()
 local third=ctx.desired:group(group_id)
 first:set_ct(300);third:set_brightness(55)
 end)`); e != nil {
		t.Fatal(e)
	}
	if e := i.Invoke(context.Background(), "alias", nil, ""); e != nil {
		t.Fatal(e)
	}
	d, _, e := stores.Groups().Get(fixtureGroup)
	if e != nil || d.On == nil || !*d.On || d.Brightness == nil || *d.Brightness != 55 || d.Ct == nil || *d.Ct != 300 {
		t.Fatalf("alias patch lost: %+v %v", d, e)
	}
}

func TestNativeDesiredValidatesBeforeCommitAndDoesNotClampLegacyCT(t *testing.T) {
	L, i, stores, _ := actionFixture(t)
	L.SetGlobal("group_id", lua.LString(fixtureGroup))
	if e := L.DoString(`local a=require("action");a.define("wide",function(ctx)ctx.desired:group(group_id):set_ct(600)end);a.define("invalid",function(ctx)ctx.desired:group(group_id):set_color(2,0.3)end)`); e != nil {
		t.Fatal(e)
	}
	if e := i.Invoke(context.Background(), "wide", nil, ""); e != nil {
		t.Fatal(e)
	}
	d, version, _ := stores.Groups().Get(fixtureGroup)
	if d.Ct == nil || *d.Ct != 600 {
		t.Fatal("legacy CT clamp remains")
	}
	if i.Invoke(context.Background(), "invalid", nil, "") == nil {
		t.Fatal("invalid XY accepted")
	}
	_, after, _ := stores.Groups().Get(fixtureGroup)
	if after != version {
		t.Fatal("invalid desired committed")
	}
}
