package modules

import (
	"github.com/rs/zerolog/log"
	lua "github.com/yuin/gopher-lua"
	"time"

	"github.com/dokzlo13/lightd/internal/geo"
)

// GeoModule provides geographical/astronomical functions to Lua
type GeoModule struct {
	calculator *geo.Calculator
}

// NewGeoModule creates a new geo module with a shared calculator.
// The calculator already contains the resolved location.
func NewGeoModule(calculator *geo.Calculator) *GeoModule {
	return &GeoModule{
		calculator: calculator,
	}
}

// Loader is the module loader for Lua
func (m *GeoModule) Loader(L *lua.LState) int {
	mod := L.NewTable()

	L.SetField(mod, "today", L.NewFunction(m.today))
	L.SetField(mod, "for_date", L.NewFunction(m.forDate))
	L.SetField(mod, "location", L.NewFunction(m.location))

	L.Push(mod)
	return 1
}

// today() -> {dawn, sunrise, noon, sunset, dusk, midnight}
// Returns Unix timestamps for astronomical events today
func (m *GeoModule) today(L *lua.LState) int {
	if !m.calculator.IsConfigured() {
		log.Warn().Msg("geo.today() called but no location configured")
		L.Push(lua.LNil)
		return 1
	}

	return m.pushDate(L, time.Now())
}
func (m *GeoModule) forDate(L *lua.LState) int {
	if !m.calculator.IsConfigured() {
		L.Push(lua.LNil)
		return 1
	}
	loc, err := time.LoadLocation(m.calculator.Location().Timezone)
	if err != nil {
		L.RaiseError("invalid timezone: %s", err)
		return 0
	}
	date, err := time.ParseInLocation("2006-01-02", L.CheckString(1), loc)
	if err != nil {
		L.RaiseError("date must be YYYY-MM-DD")
		return 0
	}
	return m.pushDate(L, date)
}
func (m *GeoModule) pushDate(L *lua.LState, date time.Time) int {
	times, err := m.calculator.GetTimes(date)
	if err != nil {
		log.Error().Err(err).Msg("Failed to calculate astronomical times")
		L.Push(lua.LNil)
		return 1
	}

	result := L.NewTable()
	if !times.Dawn.IsZero() {
		L.SetField(result, "dawn", lua.LNumber(times.Dawn.Unix()))
	}
	if !times.Sunrise.IsZero() {
		L.SetField(result, "sunrise", lua.LNumber(times.Sunrise.Unix()))
	}
	if !times.Noon.IsZero() {
		L.SetField(result, "noon", lua.LNumber(times.Noon.Unix()))
	}
	if !times.Sunset.IsZero() {
		L.SetField(result, "sunset", lua.LNumber(times.Sunset.Unix()))
	}
	if !times.Dusk.IsZero() {
		L.SetField(result, "dusk", lua.LNumber(times.Dusk.Unix()))
	}
	if !times.Midnight.IsZero() {
		L.SetField(result, "midnight", lua.LNumber(times.Midnight.Unix()))
	}

	L.Push(result)
	return 1
}

// location() -> {name, lat, lon, timezone} or nil
// Returns the configured location information
func (m *GeoModule) location(L *lua.LState) int {
	loc := m.calculator.Location()
	if loc == nil {
		L.Push(lua.LNil)
		return 1
	}

	result := L.NewTable()
	L.SetField(result, "name", lua.LString(loc.Name))
	L.SetField(result, "lat", lua.LNumber(loc.Latitude))
	L.SetField(result, "lon", lua.LNumber(loc.Longitude))
	L.SetField(result, "timezone", lua.LString(loc.Timezone))

	L.Push(result)
	return 1
}
