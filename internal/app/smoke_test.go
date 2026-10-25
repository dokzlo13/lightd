package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dokzlo13/lightd/internal/config"
)

// All identities, credentials and data in this fixture are synthetic. The real
// service stack talks only to an in-process HTTP bridge and temporary SQLite.
const (
	smokeZone    = "10000000-0000-4000-8000-000000000001"
	smokeGrouped = "10000000-0000-4000-8000-000000000002"
	smokeA       = "20000000-0000-4000-8000-000000000001"
	smokeB       = "20000000-0000-4000-8000-000000000002"
	smokeScene   = "30000000-0000-4000-8000-000000000001"
	smokeButton  = "40000000-0000-4000-8000-000000000001"
	smokeRotary  = "40000000-0000-4000-8000-000000000002"
	smokeToken   = "fixture-token-not-a-real-credential"
)

type smokeWrite struct {
	ID   string
	Body map[string]any
}
type smokeBridge struct {
	mu        sync.Mutex
	server    *httptest.Server
	lights    map[string]map[string]any
	writes    []smokeWrite
	failLight string
	events    chan string
	streams   atomic.Int32
}

func newSmokeBridge(t *testing.T) *smokeBridge {
	t.Helper()
	f := &smokeBridge{lights: map[string]map[string]any{}, events: make(chan string, 8)}
	for _, id := range []string{smokeA, smokeB} {
		f.lights[id] = map[string]any{
			"id": id, "type": "light", "metadata": map[string]any{"name": "Fixture lamp"},
			"on": map[string]any{"on": id == smokeA}, "dimming": map[string]any{"brightness": 70.0},
			"color_temperature": map[string]any{"mirek": 250.0, "mirek_valid": true, "mirek_schema": map[string]any{"mirek_minimum": 160, "mirek_maximum": 450}},
			"effects":           map[string]any{"status": "no_effect", "effect_values": []string{"no_effect", "candle"}},
		}
	}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	return f
}
func (f *smokeBridge) serve(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("hue-application-key") != smokeToken {
		http.Error(w, "fixture credential required", http.StatusForbidden)
		return
	}
	if r.URL.Path == "/eventstream/clip/v2" {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, ": fixture stream\n\n")
		w.(http.Flusher).Flush()
		f.streams.Add(1)
		defer f.streams.Add(-1)
		for {
			select {
			case <-r.Context().Done():
				return
			case event := <-f.events:
				fmt.Fprintf(w, "id: fixture-stream\ndata: %s\n\n", event)
				w.(http.Flusher).Flush()
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	f.mu.Lock()
	defer f.mu.Unlock()
	path := strings.TrimPrefix(r.URL.Path, "/clip/v2/resource/")
	if r.URL.Path == "/clip/v2/resource" {
		path = ""
	}
	var data any
	switch path {
	case "room", "device":
		data = []any{}
	case "zone":
		data = []any{map[string]any{"id": smokeZone, "type": "zone", "metadata": map[string]any{"name": "Fixture zone"}, "services": []any{map[string]any{"rid": smokeGrouped, "rtype": "grouped_light"}}, "children": []any{map[string]any{"rid": smokeA, "rtype": "light"}, map[string]any{"rid": smokeB, "rtype": "light"}}}}
	case "":
		data = []any{}
	case "scene":
		actions := []any{}
		for _, id := range []string{smokeA, smokeB} {
			actions = append(actions, map[string]any{"target": map[string]any{"rid": id, "rtype": "light"}, "action": map[string]any{"on": map[string]any{"on": true}, "dimming": map[string]any{"brightness": 35}, "color_temperature": map[string]any{"mirek": 400}}})
		}
		data = []any{map[string]any{"id": smokeScene, "type": "scene", "group": map[string]any{"rid": smokeZone, "rtype": "zone"}, "metadata": map[string]any{"name": "Fixture warm"}, "actions": actions}}
	case "light":
		data = []any{f.lights[smokeA], f.lights[smokeB]}
	default:
		id := strings.TrimPrefix(path, "light/")
		light, ok := f.lights[id]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"errors":[{"description":"fixture resource missing"}],"data":[]}`)
			return
		}
		if r.Method == http.MethodPut {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			f.writes = append(f.writes, smokeWrite{id, body})
			if id == f.failLight {
				fmt.Fprint(w, `{"errors":[{"description":"injected temporary bridge failure"}],"data":[]}`)
				return
			}
			for k, v := range body {
				if old, ok := light[k].(map[string]any); ok {
					if patch, ok := v.(map[string]any); ok {
						for key, value := range patch {
							old[key] = value
						}
						continue
					}
				}
				light[k] = v
			}
			if _, ok := body["color_temperature"]; ok {
				light["color_temperature"].(map[string]any)["mirek_valid"] = true
			}
			if _, ok := body["color"]; ok {
				light["color_temperature"].(map[string]any)["mirek_valid"] = false
			}
			data = []any{map[string]any{"rid": id, "rtype": "light"}}
		} else {
			data = []any{light}
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data, "errors": []any{}})
}
func (f *smokeBridge) state(id string) (bool, float64, float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	l := f.lights[id]
	return l["on"].(map[string]any)["on"].(bool), l["dimming"].(map[string]any)["brightness"].(float64), l["color_temperature"].(map[string]any)["mirek"].(float64)
}
func (f *smokeBridge) writeCount() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.writes) }
func (f *smokeBridge) fail(id string)  { f.mu.Lock(); f.failLight = id; f.mu.Unlock() }
func (f *smokeBridge) event(t *testing.T, id string, item map[string]any) {
	t.Helper()
	body, err := json.Marshal([]any{map[string]any{"id": id, "type": "update", "creationtime": time.Now().UTC().Format(time.RFC3339Nano), "data": []any{item}}})
	if err != nil {
		t.Fatal(err)
	}
	f.events <- string(body)
}

const smokeLua = `
local action=require("action")
local hue=require("hue")
local webhook=require("events.webhook")
local sse=require("events.sse")
local kv=require("kv"):bucket("fixture",{persistent=true})
local group_id, light_id, scene_id, button_id, rotary_id = %q,%q,%q,%q,%q
assert(kv:get("legacy_fixture")==true)
action.define("scene",function(ctx)ctx.desired:group(group_id):set_scene(scene_id):preserve_power():set_transition(0)end)
action.define("toggle",function(ctx)ctx.desired:group(group_id):toggle():set_brightness(60):set_transition(0)end)
action.define("button_old",function(ctx)kv:store("button","old")end)
action.define("button_new",function(ctx)kv:store("button","new")end)
action.define("child_mode",function(ctx)
 kv:store("mode",true)
 sse.unbind_button(button_id,"short_release")
 sse.button(button_id,"short_release","button_new",{})
end)
action.define("parent_mode",function(ctx)action.run("child_mode",{})end)
action.define("desired_ct",function(ctx)ctx.desired:group(group_id):set_ct(410):preserve_power()end)
action.define("manual",function(ctx)local light,err=hue.light(light_id);assert(light,err);light:set_brightness(18)end)
action.define("restore",function(ctx)
 local group,err=hue.group(group_id);assert(group,err)
 local snapshot=group:snapshot()
 group:set_ct(300)
 group:restore(snapshot,false)
 kv:store("restored",true)
end)
action.define("rotary",function(ctx,args)
 local light,err=hue.light(light_id);assert(light,err)
 light:set_brightness(light:get_brightness()+args.steps)
 kv:store("rotary_steps",args.steps)
end)
sse.button(button_id,"short_release","button_old",{})
sse.rotary(rotary_id,"rotary",{})
for _,name in ipairs({"scene","toggle","parent_mode","desired_ct","manual","restore"})do webhook.define("POST","/"..name,name,{})end
`

func smokePorts(t *testing.T) (int, int) {
	t.Helper()
	a, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	b, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	return a.Addr().(*net.TCPAddr).Port, b.Addr().(*net.TCPAddr).Port
}
func smokeConfig(t *testing.T, f *smokeBridge, dir string) *config.Config {
	t.Helper()
	script := filepath.Join(dir, "fixture.lua")
	if e := os.WriteFile(script, []byte(fmt.Sprintf(smokeLua, smokeZone, smokeA, smokeScene, smokeButton, smokeRotary)), 0600); e != nil {
		t.Fatal(e)
	}
	health, webhook := smokePorts(t)
	yes, no := true, false
	return &config.Config{Hue: config.HueConfig{Bridge: f.server.URL, Token: smokeToken, Timeout: config.Duration(time.Second)}, Database: config.DatabaseConfig{Path: filepath.Join(dir, "fixture.sqlite")}, Script: script, ActionTimeout: config.Duration(4 * time.Second), ShutdownTimeout: config.Duration(3 * time.Second), Healthcheck: config.HealthcheckConfig{Enabled: true, Host: "127.0.0.1", Port: health}, Events: config.EventsConfig{SSE: config.SSEConfig{Enabled: &yes, MinRetryBackoff: config.Duration(10 * time.Millisecond), MaxRetryBackoff: config.Duration(100 * time.Millisecond)}, Webhook: config.WebhookConfig{Enabled: true, Host: "127.0.0.1", Port: webhook}, Scheduler: config.SchedulerConfig{Enabled: &no}}, Reconciler: config.ReconcilerConfig{Enabled: &yes, RateLimitRPS: 1000}}
}
func seedOldSmokeDatabase(t *testing.T, path string) {
	t.Helper()
	db, e := sql.Open("sqlite3", path)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	_, e = db.Exec(`CREATE TABLE event_ledger(id INTEGER PRIMARY KEY AUTOINCREMENT,event_type TEXT NOT NULL,timestamp INTEGER NOT NULL,payload TEXT,source TEXT,idempotency_key TEXT,def_id TEXT);
 CREATE TABLE resource_state(kind TEXT NOT NULL,id TEXT NOT NULL,payload TEXT NOT NULL,version INTEGER DEFAULT 1,updated_at INTEGER NOT NULL,PRIMARY KEY(kind,id));
 CREATE TABLE kv_store(bucket TEXT NOT NULL,key TEXT NOT NULL,value TEXT NOT NULL,expires_at INTEGER,created_at INTEGER NOT NULL,updated_at INTEGER NOT NULL,PRIMARY KEY(bucket,key));
 INSERT INTO kv_store VALUES('fixture','legacy_fixture','true',NULL,0,0);
 INSERT INTO event_ledger(event_type,timestamp,payload,source,idempotency_key) VALUES('action_completed',?, '{}','fixture','legacy-fixture');`, time.Now().Unix())
	if e != nil {
		t.Fatal(e)
	}
}
func smokeWait(t *testing.T, description string, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out: %s", description)
}
func smokeReady(t *testing.T, cfg *config.Config) {
	t.Helper()
	client := &http.Client{Timeout: time.Second}
	smokeWait(t, "application readiness", func() bool {
		r, e := client.Get(fmt.Sprintf("http://127.0.0.1:%d/ready", cfg.Healthcheck.Port))
		if e != nil {
			return false
		}
		defer r.Body.Close()
		io.Copy(io.Discard, r.Body)
		return r.StatusCode == http.StatusOK
	})
}
func smokePost(t *testing.T, a *App, name string) {
	t.Helper()
	client := &http.Client{Timeout: time.Second}
	r, e := client.Post(fmt.Sprintf("http://127.0.0.1:%d/%s", a.cfg.Events.Webhook.Port, name), "application/json", strings.NewReader(`{}`))
	if e != nil {
		t.Fatal(e)
	}
	defer r.Body.Close()
	var response map[string]string
	if e = json.NewDecoder(r.Body).Decode(&response); e != nil {
		t.Fatal(e)
	}
	if r.StatusCode != http.StatusAccepted || response["event_id"] == "" {
		t.Fatalf("admission: %d %v", r.StatusCode, response)
	}
	id := response["event_id"]
	smokeWait(t, "terminal webhook action "+name, func() bool {
		var status, detail string
		e := a.services.DB.QueryRow(`SELECT i.status,i.error FROM ingress_receipts e JOIN invocations i ON e.invocation_id=i.id WHERE e.id=?`, id).Scan(&status, &detail)
		if e != nil {
			return false
		}
		if status == "failed" {
			t.Fatalf("action %s failed: %s", name, detail)
		}
		return status == "completed"
	})
}
func smokeKV(t *testing.T, a *App, key string, want any) {
	t.Helper()
	smokeWait(t, "KV "+key, func() bool {
		var raw string
		if a.services.DB.QueryRow(`SELECT value FROM kv_store WHERE bucket='fixture' AND key=?`, key).Scan(&raw) != nil {
			return false
		}
		var value any
		if json.Unmarshal([]byte(raw), &value) != nil {
			return false
		}
		return value == want
	})
}

func TestAppSyntheticHueLifecycle(t *testing.T) {
	f := newSmokeBridge(t)
	dir := t.TempDir()
	cfg := smokeConfig(t, f, dir)
	seedOldSmokeDatabase(t, cfg.Database.Path)
	if e := cfg.Validate(); e != nil {
		t.Fatal(e)
	}
	a, e := New(cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if e := a.Stop(); e != nil {
			t.Errorf("cleanup: %v", e)
		}
	})
	if e = a.Start(context.Background()); e != nil {
		t.Fatal(e)
	}
	smokeReady(t, cfg)
	smokeWait(t, "fake SSE connection", func() bool { return f.streams.Load() == 1 && a.services.Hue.EventStream.Status().Connected })
	var oldRows int
	if e = a.services.DB.QueryRow(`SELECT count(*) FROM event_ledger WHERE idempotency_key='legacy-fixture'`).Scan(&oldRows); e != nil || oldRows != 1 {
		t.Fatal("previous schema history lost", oldRows, e)
	}

	// Scene changes preserve a partially off group's actual per-member power.
	smokePost(t, a, "scene")
	smokeWait(t, "scene applied to powered member", func() bool { on, bri, ct := f.state(smokeA); return on && bri == 35 && ct == 400 })
	onB, _, _ := f.state(smokeB)
	if onB {
		t.Fatal("scene turned an off member on")
	}
	f.mu.Lock()
	for _, w := range f.writes {
		if _, ok := w.Body["on"]; ok {
			f.mu.Unlock()
			t.Fatal("preserve scene sent a power field")
		}
	}
	f.mu.Unlock()

	// Toggle reads actual group power; native brightness remains percentage.
	smokePost(t, a, "toggle")
	smokeWait(t, "group off", func() bool { a, _, _ := f.state(smokeA); b, _, _ := f.state(smokeB); return !a && !b })
	smokePost(t, a, "toggle")
	smokeWait(t, "group on at sixty percent", func() bool {
		a, ab, _ := f.state(smokeA)
		b, bb, _ := f.state(smokeB)
		return a && b && ab == 60 && bb == 60
	})

	// Exercise real SSE decoding, nested Lua actions and runtime rebinds.
	f.event(t, "fixture-before", map[string]any{"id": smokeButton, "type": "button", "button": map[string]any{"button_report": map[string]any{"event": "short_release"}}})
	smokeKV(t, a, "button", "old")
	smokePost(t, a, "parent_mode")
	smokeKV(t, a, "mode", true)
	f.event(t, "fixture-after", map[string]any{"id": smokeButton, "type": "button", "button": map[string]any{"button_report": map[string]any{"event": "short_release"}}})
	smokeKV(t, a, "button", "new")
	var nested int
	if e = a.services.DB.QueryRow(`SELECT count(*) FROM invocations WHERE action='child_mode' AND json_extract(metadata,'$.parent_id')!='' AND status='completed'`).Scan(&nested); e != nil || nested != 1 {
		t.Fatal("nested invocation identity missing", nested, e)
	}

	// A temporary partial bridge failure is retried, but a later manual command
	// supersedes the old desired member and remains authoritative across restart.
	f.fail(smokeA)
	smokePost(t, a, "desired_ct")
	smokeWait(t, "persisted reconciliation retry", func() bool { return len(a.services.Hue.Orchestrator.Status().Retrying) > 0 })
	f.fail("")
	smokePost(t, a, "manual")
	smokeWait(t, "manual brightness", func() bool { _, b, _ := f.state(smokeA); return b == 18 })
	smokeWait(t, "retry accounted without overwriting manual state", func() bool { return len(a.services.Hue.Orchestrator.Status().Retrying) == 0 })
	_, briA, _ := f.state(smokeA)
	if briA != 18 {
		t.Fatal("old retry overwrote manual state", briA)
	}

	// Snapshot/restore keeps individual member appearances; rotary is another
	// direct command and records an invocation instead of disappearing.
	_, _, beforeCTA := f.state(smokeA)
	_, _, beforeCTB := f.state(smokeB)
	smokePost(t, a, "restore")
	smokeKV(t, a, "restored", true)
	_, _, afterCTA := f.state(smokeA)
	_, _, afterCTB := f.state(smokeB)
	if beforeCTA != afterCTA || beforeCTB != afterCTB {
		t.Fatal("snapshot collapsed member appearances")
	}
	f.event(t, "fixture-rotary", map[string]any{"id": smokeRotary, "type": "relative_rotary", "relative_rotary": map[string]any{"last_event": map[string]any{"action": "start", "rotation": map[string]any{"direction": "clock_wise", "steps": 2, "duration": 10}}}})
	smokeKV(t, a, "rotary_steps", float64(2))
	_, briA, _ = f.state(smokeA)
	if briA != 20 {
		t.Fatalf("native rotary brightness=%v", briA)
	}

	if e = a.Stop(); e != nil {
		t.Fatal(e)
	}
	if e = a.Stop(); e != nil {
		t.Fatal("second stop", e)
	}
	if a.services.Lua.Status().Ready {
		t.Fatal("ready after shutdown")
	}
	if e = a.services.DB.Ping(); e == nil {
		t.Fatal("SQLite owner not closed")
	}
	smokeWait(t, "SSE shutdown", func() bool { return f.streams.Load() == 0 })
	beforeRestart := f.writeCount()
	// Reopen the same persisted database and start the whole app again.
	restarted, e := New(cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if e := restarted.Stop(); e != nil {
			t.Errorf("restart cleanup: %v", e)
		}
	})
	if e = restarted.Start(context.Background()); e != nil {
		t.Fatal(e)
	}
	smokeReady(t, cfg)
	smokeWait(t, "restart reconciler running", func() bool { return restarted.services.Hue.Orchestrator.Status().Running })
	// Observe beyond the retry timer, not merely immediately after startup.
	time.Sleep(1100 * time.Millisecond)
	if got := f.writeCount(); got != beforeRestart {
		t.Fatalf("restart replayed acknowledged/superseded effects: %d -> %d", beforeRestart, got)
	}
	_, briA, _ = f.state(smokeA)
	if briA != 20 {
		t.Fatal("restart lost manual authority", briA)
	}
	smokeKV(t, restarted, "restored", true)
	if e = restarted.Stop(); e != nil {
		t.Fatal(e)
	}
}
