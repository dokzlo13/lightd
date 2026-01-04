package modules

import (
	"github.com/rs/zerolog/log"
	lua "github.com/yuin/gopher-lua"

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

	times, err := m.calculator.GetTimesForToday()
	if err != nil {
		log.Error().Err(err).Msg("Failed to calculate astronomical times")
		L.Push(lua.LNil)
		return 1
	}

	result := L.NewTable()
	L.SetField(result, "dawn", lua.LNumber(times.Dawn.Unix()))
	L.SetField(result, "sunrise", lua.LNumber(times.Sunrise.Unix()))
	L.SetField(result, "noon", lua.LNumber(times.Noon.Unix()))
	L.SetField(result, "sunset", lua.LNumber(times.Sunset.Unix()))
	L.SetField(result, "dusk", lua.LNumber(times.Dusk.Unix()))
	L.SetField(result, "midnight", lua.LNumber(times.Midnight.Unix()))

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
