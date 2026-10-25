package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/dokzlo13/lightd/internal/hue/bridge"
	lua "github.com/yuin/gopher-lua"
)

func TestNativeHueLuaSparseCommandsAndFailures(t *testing.T) {
	const id = "20000000-0000-0000-0000-000000000001"
	var mu sync.Mutex
	var payloads []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "GET" {
			fmt.Fprintf(w, `{"data":[{"id":%q,"on":{"on":true},"dimming":{"brightness":80},"color_temperature":{"mirek":250,"mirek_valid":true,"mirek_schema":{"mirek_minimum":153,"mirek_maximum":500}}}],"errors":[]}`, id)
			return
		}
		var p map[string]any
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			t.Error(err)
		}
		mu.Lock()
		payloads = append(payloads, p)
		mu.Unlock()
		fmt.Fprintf(w, `{"data":[{"rid":%q,"rtype":"light"}],"errors":[]}`, id)
	}))
	defer server.Close()
	c, err := bridge.NewWithHTTP(server.URL, "fixture-token", server.Client(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	L := lua.NewState()
	defer L.Close()
	L.SetContext(context.Background())
	L.PreloadModule("hue", NewHueModule(c).Loader)
	source := fmt.Sprintf(`local hue=require("hue")
 local lamp,err=hue.light(%q);assert(lamp and not err)
 assert(lamp:id()==%q)
 lamp:set_state({on=false,brightness=0,duration=0})
 assert(not pcall(function() lamp:set_state({bri=254}) end))
 assert(not pcall(function() lamp:set_state({unknown_field=true}) end))
 assert(not pcall(function() lamp:set_brightness(101) end))
 assert(not pcall(function() lamp:effect("colorloop") end))
 local saved=lamp:snapshot();assert(saved.lights[%q].brightness==80)
 lamp:restore(saved)
 local invalid,why=hue.light("2");assert(invalid==nil and type(why)=="string")`, id, id, id)
	if err := L.DoString(source); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(payloads) != 2 {
		t.Fatalf("unexpected writes: %v", payloads)
	}
	p := payloads[0]
	if p["on"].(map[string]any)["on"] != false || p["dimming"].(map[string]any)["brightness"] != float64(0) || p["dynamics"].(map[string]any)["duration"] != float64(0) {
		t.Fatalf("zero/false lost: %v", p)
	}
	if _, ok := payloads[1]["on"]; ok {
		t.Fatal("restore invented a power command")
	}
}
