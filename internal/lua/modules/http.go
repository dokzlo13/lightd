package modules

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	lua "github.com/yuin/gopher-lua"

	"github.com/dokzlo13/lightd/internal/config"
)

// HTTPModule provides HTTP client functionality to Lua scripts
type HTTPModule struct {
	enabled         bool
	client          *http.Client
	cfg             *config.HTTPClientConfig
	maxResponseSize int64
}

// NewHTTPModule creates a new HTTP module
func NewHTTPModule(cfg *config.HTTPClientConfig) *HTTPModule {
	enabled := cfg.IsEnabled()

	var client *http.Client
	if enabled {
		transport := &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: cfg.InsecureSkipVerify,
			},
		}

		client = &http.Client{
			Timeout:   cfg.GetTimeout(),
			Transport: transport,
		}

		log.Info().
			Dur("timeout", cfg.GetTimeout()).
			Str("allow_domains", cfg.AllowDomains).
			Msg("HTTP client enabled")
	} else {
		log.Info().Msg("HTTP client disabled")
	}

	return &HTTPModule{
		enabled:         enabled,
		client:          client,
		cfg:             cfg,
		maxResponseSize: cfg.GetMaxResponseSize(),
	}
}

// Loader is the module loader for Lua
func (m *HTTPModule) Loader(L *lua.LState) int {
	if !m.enabled {
		L.RaiseError("clients.http module is disabled (clients.http.enabled: false in config)")
		return 0
	}

	mod := L.NewTable()

	L.SetField(mod, "get", L.NewFunction(m.get))
	L.SetField(mod, "post", L.NewFunction(m.post))
	L.SetField(mod, "put", L.NewFunction(m.put))
	L.SetField(mod, "delete", L.NewFunction(m.delete))
	L.SetField(mod, "patch", L.NewFunction(m.patch))
	L.SetField(mod, "request", L.NewFunction(m.request))

	L.Push(mod)
	return 1
}

// get(url, opts?) -> response, error
func (m *HTTPModule) get(L *lua.LState) int {
	urlStr := L.CheckString(1)
	opts := L.OptTable(2, nil)

	return m.doRequest(L, "GET", urlStr, "", opts)
}

// post(url, body, opts?) -> response, error
func (m *HTTPModule) post(L *lua.LState) int {
	urlStr := L.CheckString(1)
	body := L.OptString(2, "")
	opts := L.OptTable(3, nil)

	return m.doRequest(L, "POST", urlStr, body, opts)
}

// put(url, body, opts?) -> response, error
func (m *HTTPModule) put(L *lua.LState) int {
	urlStr := L.CheckString(1)
	body := L.OptString(2, "")
	opts := L.OptTable(3, nil)

	return m.doRequest(L, "PUT", urlStr, body, opts)
}

// delete(url, opts?) -> response, error
func (m *HTTPModule) delete(L *lua.LState) int {
	urlStr := L.CheckString(1)
	opts := L.OptTable(2, nil)

	return m.doRequest(L, "DELETE", urlStr, "", opts)
}

// patch(url, body, opts?) -> response, error
func (m *HTTPModule) patch(L *lua.LState) int {
	urlStr := L.CheckString(1)
	body := L.OptString(2, "")
	opts := L.OptTable(3, nil)

	return m.doRequest(L, "PATCH", urlStr, body, opts)
}

// request(method, url, body?, opts?) -> response, error
// Generic request method for full control
func (m *HTTPModule) request(L *lua.LState) int {
	method := strings.ToUpper(L.CheckString(1))
	urlStr := L.CheckString(2)
	body := L.OptString(3, "")
	opts := L.OptTable(4, nil)

	return m.doRequest(L, method, urlStr, body, opts)
}

// doRequest performs the HTTP request and returns results to Lua
func (m *HTTPModule) doRequest(L *lua.LState, method, urlStr, body string, opts *lua.LTable) int {
	// Parse and validate URL
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(fmt.Sprintf("invalid URL: %s", err.Error())))
		return 2
	}

	// Check domain allowlist
	if !m.cfg.IsDomainAllowed(parsedURL.Hostname()) {
		L.Push(lua.LNil)
		L.Push(lua.LString(fmt.Sprintf("domain not allowed: %s", parsedURL.Hostname())))
		return 2
	}

	// Create request
	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}

	req, err := http.NewRequest(method, urlStr, bodyReader)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(fmt.Sprintf("failed to create request: %s", err.Error())))
		return 2
	}

	// Set default headers
	req.Header.Set("User-Agent", "lightd/1.0")

	// Apply options
	timeout := m.cfg.GetTimeout()
	if opts != nil {
		// Parse headers from options
		if headersVal := opts.RawGetString("headers"); headersVal != lua.LNil {
			if headersTbl, ok := headersVal.(*lua.LTable); ok {
				headersTbl.ForEach(func(k, v lua.LValue) {
					if ks, ok := k.(lua.LString); ok {
						req.Header.Set(string(ks), lua.LVAsString(v))
					}
				})
			}
		}

		// Parse timeout override (in milliseconds)
		if timeoutVal := opts.RawGetString("timeout"); timeoutVal != lua.LNil {
			if timeoutMs, ok := timeoutVal.(lua.LNumber); ok {
				timeout = time.Duration(timeoutMs) * time.Millisecond
			}
		}
	}

	// Create client with potential timeout override
	client := m.client
	if timeout != m.cfg.GetTimeout() {
		client = &http.Client{
			Timeout:   timeout,
			Transport: m.client.Transport,
		}
	}

	log.Debug().
		Str("method", method).
		Str("url", urlStr).
		Dur("timeout", timeout).
		Msg("HTTP request")

	// Perform request
	resp, err := client.Do(req)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(fmt.Sprintf("request failed: %s", err.Error())))
		return 2
	}
	defer resp.Body.Close()

	// Read response body with size limit
	limitedReader := io.LimitReader(resp.Body, m.maxResponseSize)
	respBody, err := io.ReadAll(limitedReader)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(fmt.Sprintf("failed to read response: %s", err.Error())))
		return 2
	}

	// Build response table
	respTable := L.NewTable()

	// Status
	L.SetField(respTable, "status", lua.LNumber(resp.StatusCode))
	L.SetField(respTable, "status_text", lua.LString(resp.Status))

	// Body
	L.SetField(respTable, "body", lua.LString(string(respBody)))

	// Try to parse JSON if content-type suggests it
	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "application/json") {
		var jsonData interface{}
		if err := json.Unmarshal(respBody, &jsonData); err == nil {
			L.SetField(respTable, "json", GoToLuaValue(L, jsonData))
		} else {
			L.SetField(respTable, "json", lua.LNil)
		}
	} else {
		L.SetField(respTable, "json", lua.LNil)
	}

	// Response headers
	headersTable := L.NewTable()
	for key, values := range resp.Header {
		if len(values) == 1 {
			L.SetField(headersTable, key, lua.LString(values[0]))
		} else {
			// Multiple values - join with comma
			L.SetField(headersTable, key, lua.LString(strings.Join(values, ", ")))
		}
	}
	L.SetField(respTable, "headers", headersTable)

	// Success indicator
	L.SetField(respTable, "ok", lua.LBool(resp.StatusCode >= 200 && resp.StatusCode < 300))

	L.Push(respTable)
	L.Push(lua.LNil)
	return 2
}
