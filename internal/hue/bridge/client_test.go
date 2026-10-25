package bridge

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/time/rate"
)

const zoneID = "10000000-0000-0000-0000-000000000001"
const groupedID = "10000000-0000-0000-0000-000000000002"
const lightA = "20000000-0000-0000-0000-000000000001"
const lightB = "20000000-0000-0000-0000-000000000002"
const sceneID = "30000000-0000-0000-0000-000000000001"

type write struct {
	ID   string
	Body map[string]any
}
type fixture struct {
	putEntered chan struct{}
	putRelease chan struct{}
	blockOnce  sync.Once
	mu         sync.Mutex
	server     *httptest.Server
	lights     map[string]map[string]any
	writes     []write
	failID     string
	status     int
	slow       bool
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{lights: map[string]map[string]any{}}
	for _, id := range []string{lightA, lightB} {
		f.lights[id] = map[string]any{"id": id, "on": map[string]any{"on": true}, "dimming": map[string]any{"brightness": 70}, "color_temperature": map[string]any{"mirek": 250, "mirek_valid": true, "mirek_schema": map[string]any{"mirek_minimum": 160, "mirek_maximum": 450}}, "effects": map[string]any{"effect_values": []string{"no_effect", "candle"}, "status": "no_effect"}, "signaling": map[string]any{"signal_values": []string{"on_off", "no_signal"}}}
	}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	return f
}
func (f *fixture) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.slow {
		f.mu.Unlock()
		<-r.Context().Done()
		f.mu.Lock()
		return
	}
	if f.status != 0 {
		w.WriteHeader(f.status)
		fmt.Fprint(w, `{"data":[],"errors":[{"description":"fixture rejected"}]}`)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/clip/v2/resource/")
	if r.URL.Path == "/clip/v2/resource" {
		path = ""
	}
	var data any
	switch path {
	case "room":
		data = []any{}
	case "zone":
		data = []any{map[string]any{"id": zoneID, "metadata": map[string]any{"name": "Test zone"}, "services": []any{map[string]any{"rid": groupedID, "rtype": "grouped_light"}}, "children": []any{map[string]any{"rid": lightA, "rtype": "light"}, map[string]any{"rid": lightB, "rtype": "light"}}}}
	case "":
		data = []any{map[string]any{"type": "some_future_resource", "status": map[string]any{"active": true}}}
	case "device":
		data = []any{}
	case "scene":
		actions := []any{}
		for _, id := range []string{lightA, lightB} {
			actions = append(actions, map[string]any{"target": map[string]any{"rid": id, "rtype": "light"}, "action": map[string]any{"on": map[string]any{"on": true}, "dimming": map[string]any{"brightness": 35}, "color_temperature": map[string]any{"mirek": 400}}})
		}
		data = []any{map[string]any{"id": sceneID, "group": map[string]any{"rid": zoneID, "rtype": "zone"}, "metadata": map[string]any{"name": "Relax"}, "actions": actions}}
	case "light":
		data = []any{f.lights[lightA], f.lights[lightB]}
	default:
		id := strings.TrimPrefix(path, "light/")
		l, ok := f.lights[id]
		if !ok {
			w.WriteHeader(404)
			fmt.Fprint(w, `{"data":[],"errors":[{"description":"not found"}]}`)
			return
		}
		if r.Method == "PUT" {
			if f.putEntered != nil {
				f.blockOnce.Do(func() { close(f.putEntered); <-f.putRelease })
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				panic(err)
			}
			f.writes = append(f.writes, write{id, body})
			if f.failID == id {
				fmt.Fprint(w, `{"data":[],"errors":[{"description":"temporary failure"}]}`)
				return
			}
			for k, v := range body {
				if k == "color_temperature" {
					ct := v.(map[string]any)
					ct["mirek_valid"] = true
					ct["mirek_schema"] = l[k].(map[string]any)["mirek_schema"]
				}
				l[k] = v
			}
			data = []any{map[string]any{"rid": id, "rtype": "light"}}
		} else {
			data = []any{l}
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data, "errors": []any{}})
}
func (f *fixture) client(t *testing.T, db *sql.DB) *Client {
	t.Helper()
	c, err := NewWithHTTP(f.server.URL, "fixture-token", f.server.Client(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	c.lightLimit = rate.NewLimiter(rate.Inf, 1)
	if err = c.SetDB(db); err != nil {
		t.Fatal(err)
	}
	if err = c.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	return c
}
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`CREATE TABLE resource_state(kind TEXT,id TEXT,version INTEGER,payload TEXT,PRIMARY KEY(kind,id))`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
func TestSparseScenePreservesPartialGroupPower(t *testing.T) {
	f := newFixture(t)
	f.lights[lightB]["on"] = map[string]any{"on": false}
	c := f.client(t, nil)
	zero := 0
	if err := c.ApplyGroup(context.Background(), zoneID, State{PowerMode: "preserve", Duration: &zero}, sceneID); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.writes) != 1 || f.writes[0].ID != lightA {
		t.Fatalf("writes=%v", f.writes)
	}
	p := f.writes[0].Body
	if _, ok := p["on"]; ok {
		t.Fatalf("unexpected power field: %v", p)
	}
	if p["dynamics"].(map[string]any)["duration"] != float64(0) {
		t.Fatalf("zero duration lost: %v", p)
	}
}
func TestManualFenceSurvivesRestartAndOnlySuppressesAffectedMember(t *testing.T) {
	f := newFixture(t)
	db := openTestDB(t)
	if _, err := db.Exec(`INSERT INTO resource_state VALUES('group',?,1,'{}')`, zoneID); err != nil {
		t.Fatal(err)
	}
	c := f.client(t, db)
	publish(t, c, db, "group", zoneID, 1)
	ctx := WithDesired(context.Background(), "group", zoneID, 1)
	ct := uint16(400)
	f.failID = lightA
	if err := c.ApplyGroup(ctx, zoneID, State{Ct: &ct}, ""); err == nil {
		t.Fatal("expected partial failure")
	}
	f.failID = ""
	b := 20.0
	if err := c.ApplyLight(context.Background(), lightA, State{Brightness: &b}); err != nil {
		t.Fatal(err)
	}
	before := len(f.writes)
	restarted := f.client(t, db)
	if err := restarted.ApplyGroup(ctx, zoneID, State{Ct: &ct}, ""); err != nil {
		t.Fatal(err)
	}
	for _, w := range f.writes[before:] {
		if w.ID == lightA {
			t.Fatalf("old retry overwrote manual member: %v", w)
		}
	}
	if len(f.writes) != before {
		t.Fatalf("acknowledged member replayed after restart: %v", f.writes[before:])
	}
	if _, err := db.Exec(`UPDATE resource_state SET version=2 WHERE kind='group' AND id=?`, zoneID); err != nil {
		t.Fatal(err)
	}
	publish(t, restarted, db, "group", zoneID, 2)
	before = len(f.writes)
	if err := restarted.ApplyGroup(WithDesired(context.Background(), "group", zoneID, 2), zoneID, State{Ct: &ct}, ""); err != nil {
		t.Fatal(err)
	}
	if len(f.writes) != before+2 {
		t.Fatal("new desired version did not regain authority")
	}
}
func TestSupersededVersionAndNextOnNeverWrite(t *testing.T) {
	f := newFixture(t)
	db := openTestDB(t)
	_, _ = db.Exec(`INSERT INTO resource_state VALUES('light',?,2,'{}')`, lightA)
	c := f.client(t, db)
	publish(t, c, db, "light", lightA, 2)
	b := 40.0
	if err := c.ApplyLight(WithDesired(context.Background(), "light", lightA, 1), lightA, State{Brightness: &b}); !errors.Is(err, ErrSuperseded) {
		t.Fatal(err)
	}
	if err := c.ApplyLight(context.Background(), lightA, State{Brightness: &b, PowerMode: "next_on"}); err != nil {
		t.Fatal(err)
	}
	if len(f.writes) != 0 {
		t.Fatal(f.writes)
	}
}
func TestErrorsAndCancellationAreObservable(t *testing.T) {
	f := newFixture(t)
	c := f.client(t, nil)
	b := 30.0
	f.failID = lightA
	if err := c.ApplyLight(context.Background(), lightA, State{Brightness: &b}); err == nil {
		t.Fatal("HTTP200 error envelope accepted")
	}
	f.failID = ""
	f.status = 403
	if _, err := c.Light(context.Background(), lightA); err == nil {
		t.Fatal("HTTP403 accepted")
	}
	f.status = 0
	f.slow = true
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := c.Light(ctx, lightA); err == nil {
		t.Fatal("cancellation ignored")
	}
}
func TestSnapshotRestoresEachMemberAppearanceWithoutPower(t *testing.T) {
	f := newFixture(t)
	f.lights[lightB]["color_temperature"].(map[string]any)["mirek"] = 300
	c := f.client(t, nil)
	snap, err := c.Snapshot(context.Background(), "group", zoneID)
	if err != nil {
		t.Fatal(err)
	}
	before := len(f.writes)
	if err = c.Restore(context.Background(), snap, false); err != nil {
		t.Fatal(err)
	}
	if len(f.writes) != before+2 {
		t.Fatal("restore missing member")
	}
	for _, w := range f.writes[before:] {
		if _, ok := w.Body["on"]; ok {
			t.Fatal("restore changed power")
		}
		want := 250.0
		if w.ID == lightB {
			want = 300
		}
		if w.Body["color_temperature"].(map[string]any)["mirek"] != want {
			t.Fatalf("member color lost %v", w)
		}
	}
}
func TestNativeValidationAndCapabilities(t *testing.T) {
	f := newFixture(t)
	c := f.client(t, nil)
	b := 0.0
	off := false
	if err := c.ApplyLight(context.Background(), lightA, State{Brightness: &b, On: &off}); err != nil {
		t.Fatal(err)
	}
	p := f.writes[0].Body
	if p["on"].(map[string]any)["on"] != false || p["dimming"].(map[string]any)["brightness"] != float64(0) {
		t.Fatalf("sparse zero/false lost: %v", p)
	}
	if err := c.ApplyLight(context.Background(), lightA, State{Effect: "colorloop"}); err == nil {
		t.Fatal("unsupported effect accepted")
	}
	legacy := uint8(254)
	if err := c.ApplyLight(context.Background(), lightA, State{Bri: &legacy}); err == nil {
		t.Fatal("unmigrated legacy unit accepted")
	}
	if err := c.ApplyLight(context.Background(), "2", State{}); err == nil {
		t.Fatal("numeric ID accepted")
	}
}
func TestHSVConversion(t *testing.T) {
	red, err := HSVToXY(0, 100)
	if err != nil || red[0] < 0.6 || red[1] > 0.4 {
		t.Fatalf("red=%v err=%v", red, err)
	}
	if _, err = HSVToXY(0, 101); err == nil {
		t.Fatal("invalid HSV accepted")
	}
}

func publish(t *testing.T, c *Client, db *sql.DB, kind, id string, version int64) {
	t.Helper()
	release, err := c.AcquireCommand(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err = c.RecordDesiredTx(tx, kind, id, version); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestOverlappingDesiredResourcesRespectLatestMemberAuthority(t *testing.T) {
	f := newFixture(t)
	db := openTestDB(t)
	_, _ = db.Exec(`INSERT INTO resource_state VALUES('group',?,1,'{}'),('light',?,1,'{}')`, zoneID, lightA)
	c := f.client(t, db)
	publish(t, c, db, "group", zoneID, 1)
	publish(t, c, db, "light", lightA, 1)
	v := 60.0
	if err := c.ApplyGroup(WithDesired(context.Background(), "group", zoneID, 1), zoneID, State{Brightness: &v}, ""); err != nil {
		t.Fatal(err)
	}
	if len(f.writes) != 1 || f.writes[0].ID != lightB {
		t.Fatalf("old group overwrote newer light intent: %v", f.writes)
	}
}
func TestCancellationWhileCommandGateIsOccupied(t *testing.T) {
	f := newFixture(t)
	c := f.client(t, nil)
	release, err := c.AcquireCommand(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	b := 50.0
	start := time.Now()
	err = c.ApplyLight(ctx, lightA, State{Brightness: &b})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v", err)
	}
	if time.Since(start) > 200*time.Millisecond {
		t.Fatal("manual request ignored cancellation waiting for gate")
	}
}
func TestWriteRequiresSpecificAcknowledgment(t *testing.T) {
	for _, body := range []string{`{"data":[],"errors":[]}`, `{"data":{},"errors":[]}`, `{"data":[{"rid":"wrong","rtype":"light"}],"errors":[]}`} {
		if err := checkWrite(200, []byte(body), lightA); err == nil {
			t.Fatalf("accepted malformed ack: %s", body)
		}
	}
}

type fixtureRoundTripper func(*http.Request) (*http.Response, error)

func (f fixtureRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type endlessBody struct{ count int }

func (b *endlessBody) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	b.count += len(p)
	return len(p), nil
}
func (b *endlessBody) Close() error { return nil }
func TestSDKResponseBodyIsBoundedBeforeParsing(t *testing.T) {
	body := &endlessBody{}
	h := &http.Client{Transport: fixtureRoundTripper(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: body}, nil
	})}
	c, err := NewWithHTTP("https://fixture.invalid", "fixture-token", h, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Lights(context.Background()); err == nil {
		t.Fatal("invalid oversized response accepted")
	}
	if body.count > (8<<20)+1 {
		t.Fatalf("SDK read unbounded body: %d", body.count)
	}
}

func TestPublicationCannotPassAnInFlightSend(t *testing.T) {
	f := newFixture(t)
	db := openTestDB(t)
	_, _ = db.Exec(`INSERT INTO resource_state VALUES('light',?,1,'{}')`, lightA)
	c := f.client(t, db)
	publish(t, c, db, "light", lightA, 1)
	f.putEntered = make(chan struct{})
	f.putRelease = make(chan struct{})
	old := WithDesired(context.Background(), "light", lightA, 1)
	b := 30.0
	sent := make(chan error, 1)
	go func() { sent <- c.ApplyLight(old, lightA, State{Brightness: &b}) }()
	<-f.putEntered
	acquired := make(chan struct{})
	published := make(chan error, 1)
	go func() {
		release, err := c.AcquireCommand(context.Background())
		if err != nil {
			published <- err
			return
		}
		defer release()
		close(acquired)
		tx, err := db.Begin()
		if err != nil {
			published <- err
			return
		}
		defer tx.Rollback()
		if _, err = tx.Exec(`UPDATE resource_state SET version=2 WHERE kind='light' AND id=?`, lightA); err == nil {
			err = c.RecordDesiredTx(tx, "light", lightA, 2)
		}
		if err == nil {
			err = tx.Commit()
		}
		published <- err
	}()
	select {
	case <-acquired:
		t.Fatal("new desired publication passed an in-flight Hue write")
	case <-time.After(20 * time.Millisecond):
	}
	close(f.putRelease)
	if err := <-sent; err != nil {
		t.Fatal(err)
	}
	if err := <-published; err != nil {
		t.Fatal(err)
	}
	before := len(f.writes)
	if err := c.ApplyLight(old, lightA, State{Brightness: &b}); !errors.Is(err, ErrSuperseded) {
		t.Fatalf("old intent was not superseded: %v", err)
	}
	if len(f.writes) != before {
		t.Fatal("old retry sent after newer publication")
	}
}
