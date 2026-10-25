package modules

import (
	"context"
	"github.com/dokzlo13/lightd/internal/config"
	"github.com/dokzlo13/lightd/internal/lua/modules/collect"
	lua "github.com/yuin/gopher-lua"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPResponseLimitAndCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("oversize")) }))
	defer srv.Close()
	L := lua.NewState()
	defer L.Close()
	m := NewHTTPModule(&config.HTTPClientConfig{})
	m.maxResponseSize = 4
	L.PreloadModule("clients.http", m.Loader)
	L.SetGlobal("url", lua.LString(srv.URL))
	if e := L.DoString(`local h=require("clients.http");local r,e=h.get(url);assert(r==nil and string.find(e,"size limit"))`); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	L.SetContext(ctx)
	L.SetTop(0)
	L.Push(lua.LString(srv.URL))
	m.get(L)
	if L.Get(-2) != lua.LNil {
		t.Fatal("request ignored cancelled context")
	}
}
func TestHTTPRedirectRevalidatesDomain(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("disallowed redirect reached target") }))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer origin.Close()
	url := "http://localhost" + origin.URL[len("http://127.0.0.1"):]
	L := lua.NewState()
	defer L.Close()
	m := NewHTTPModule(&config.HTTPClientConfig{AllowDomains: "localhost"})
	L.PreloadModule("clients.http", m.Loader)
	L.SetGlobal("url", lua.LString(url))
	if e := L.DoString(`local r,e=require("clients.http").get(url);assert(r==nil and e)`); e != nil {
		t.Fatal(e)
	}
}
func TestSleepCancelsAndMiddlewareIsNotSerialized(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	ctx, cancel := context.WithCancel(context.Background())
	L.SetContext(ctx)
	go func() { time.Sleep(10 * time.Millisecond); cancel() }()
	m := NewUtilsModule()
	L.PreloadModule("utils", m.Loader)
	start := time.Now()
	if e := L.DoString(`require("utils").sleep(10000)`); e == nil {
		t.Fatal("cancelled sleep succeeded")
	}
	if time.Since(start) > time.Second {
		t.Fatal("sleep ignored cancellation")
	}
	L.RemoveContext()
	cm := collect.NewModule()
	L.PreloadModule("collect", cm.Loader)
	sm := NewSSEModule(true)
	L.PreloadModule("events.sse", sm.Loader)
	if e := L.DoString(`require("events.sse").button("fixture","short_release","test",{middleware=require("collect").quiet(50,function(e)return {count=#e}end)})`); e != nil {
		t.Fatal("collector capability rejected as JSON", e)
	}
}
