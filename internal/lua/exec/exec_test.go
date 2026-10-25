package exec

import (
	lua "github.com/yuin/gopher-lua"
	"testing"
)

func TestConversionRejectsCyclicSparseAndNonpositiveKeys(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	for _, script := range []string{`v={[0]="zero",[1]="one"}`, `v={[1000000000]="huge"}`, `v={};v.self=v`, `v={[1]="array",key="object"}`} {
		if e := L.DoString(script); e != nil {
			t.Fatal(e)
		}
		if _, e := ToGo(L.GetGlobal("v")); e == nil {
			t.Fatalf("accepted invalid %s", script)
		}
	}
	if e := L.DoString(`v={name="fixture",values={0,false,3}}`); e != nil {
		t.Fatal(e)
	}
	if _, e := ToGo(L.GetGlobal("v")); e != nil {
		t.Fatal(e)
	}
}
func TestReducerFailureReturnsError(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	if e := L.DoString(`function reducer(events) error("fixture") end`); e != nil {
		t.Fatal(e)
	}
	args, e := CallReducer(L, L.GetGlobal("reducer").(*lua.LFunction), nil)
	if e == nil || args != nil {
		t.Fatal(args, e)
	}
}
