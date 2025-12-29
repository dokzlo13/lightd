package context

import (
	lua "github.com/yuin/gopher-lua"
)

// requestContextKey is the key used to store request data in Go's context.Context
type requestContextKey struct{}

// RequestContextKey is exported for use by other packages
var RequestContextKey = requestContextKey{}

// RequestData holds HTTP request information for webhook-triggered actions
type RequestData struct {
	Method     string
	Path       string
	Body       string
	JSON       map[string]interface{}
	Headers    map[string]interface{}
	PathParams map[string]string
}

// RequestModule provides ctx.request for accessing HTTP request data.
//
// For webhook-triggered actions, ctx.request contains:
//   - method: HTTP method (e.g., "POST")
//   - path: Request path (e.g., "/lights/toggle")
//   - body: Raw request body string
//   - json: Parsed JSON body as table (nil if parsing fails)
//   - headers: Table of request headers
//   - path_params: Table of path parameters (e.g., {id = "123"} for "/group/{id}")
//
// For non-webhook actions, ctx.request is nil.
//
// Example Lua usage:
//
//	if ctx.request then
//	    local method = ctx.request.method
//	    local data = ctx.request.json
//	    local groupId = ctx.request.path_params.id
//	end
type RequestModule struct{}

// NewRequestModule creates a new request module.
func NewRequestModule() *RequestModule {
	return &RequestModule{}
}

// Name returns "request" - the field name in ctx.
func (m *RequestModule) Name() string {
	return "request"
}

// Install adds ctx.request to the context table.
// Uses L.Context() to extract request data if present.
func (m *RequestModule) Install(L *lua.LState, ctx *lua.LTable) {
	// Get request data from Go context
	goCtx := L.Context()
	if goCtx == nil {
		L.SetField(ctx, m.Name(), lua.LNil)
		return
	}

	reqData, ok := goCtx.Value(RequestContextKey).(*RequestData)
	if !ok || reqData == nil {
		L.SetField(ctx, m.Name(), lua.LNil)
		return
	}

	// Build request table
	request := L.NewTable()
	L.SetField(request, "method", lua.LString(reqData.Method))
	L.SetField(request, "path", lua.LString(reqData.Path))
	L.SetField(request, "body", lua.LString(reqData.Body))

	// Convert JSON to Lua table
	if reqData.JSON != nil {
		jsonTable := mapToLuaTable(L, reqData.JSON)
		L.SetField(request, "json", jsonTable)
	} else {
		L.SetField(request, "json", lua.LNil)
	}

	// Convert headers to Lua table
	if reqData.Headers != nil {
		headersTable := mapToLuaTable(L, reqData.Headers)
		L.SetField(request, "headers", headersTable)
	} else {
		L.SetField(request, "headers", lua.LNil)
	}

	// Convert path params to Lua table
	if reqData.PathParams != nil && len(reqData.PathParams) > 0 {
		pathParamsTable := L.NewTable()
		for k, v := range reqData.PathParams {
			pathParamsTable.RawSetString(k, lua.LString(v))
		}
		L.SetField(request, "path_params", pathParamsTable)
	} else {
		L.SetField(request, "path_params", L.NewTable()) // Empty table, not nil
	}

	L.SetField(ctx, m.Name(), request)
}

// mapToLuaTable converts a Go map to a Lua table
func mapToLuaTable(L *lua.LState, m map[string]interface{}) *lua.LTable {
	tbl := L.NewTable()
	for k, v := range m {
		tbl.RawSetString(k, goToLuaValue(L, v))
	}
	return tbl
}

// goToLuaValue converts a Go value to a Lua value
func goToLuaValue(L *lua.LState, v interface{}) lua.LValue {
	if v == nil {
		return lua.LNil
	}

	switch val := v.(type) {
	case string:
		return lua.LString(val)
	case float64:
		return lua.LNumber(val)
	case bool:
		return lua.LBool(val)
	case int:
		return lua.LNumber(val)
	case int64:
		return lua.LNumber(val)
	case map[string]interface{}:
		return mapToLuaTable(L, val)
	case []interface{}:
		tbl := L.NewTable()
		for i, item := range val {
			tbl.RawSetInt(i+1, goToLuaValue(L, item))
		}
		return tbl
	default:
		return lua.LNil
	}
}
