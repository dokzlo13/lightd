package modules

import (
	"time"

	"github.com/rs/zerolog/log"
	lua "github.com/yuin/gopher-lua"

	"github.com/dokzlo13/lightd/internal/geo"
)

// GeoModule provides geographical/astronomical functions to Lua
type GeoModule struct {
	defaultLocation string
	defaultTimezone string
	calculator      *geo.Calculator
}

// NewGeoModule creates a new geo module with a shared calculator
func NewGeoModule(defaultLocation, defaultTimezone string, calculator *geo.Calculator) *GeoModule {
	return &GeoModule{
		defaultLocation: defaultLocation,
		defaultTimezone: defaultTimezone,
		calculator:      calculator,
	}
}

// Loader is the module loader for Lua
func (m *GeoModule) Loader(L *lua.LState) int {
	mod := L.NewTable()

	L.SetField(mod, "today", L.NewFunction(m.today))

	L.Push(mod)
	return 1
}

// today(location?) -> {dawn, sunrise, noon, sunset, dusk, midnight}
// Returns Unix timestamps for astronomical events
func (m *GeoModule) today(L *lua.LState) int {
	location := L.OptString(1, m.defaultLocation)

	times, err := m.calculator.GetTimesForToday(location, m.defaultTimezone)
	if err != nil {
		log.Error().Err(err).Str("location", location).Msg("Failed to calculate astronomical times")
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

	// Also add formatted times for debugging
	tz, _ := time.LoadLocation(m.defaultTimezone)
	if tz == nil {
		tz = time.UTC
	}

	log.Info().
		Str("location", location).
		Str("dawn", times.Dawn.In(tz).Format("15:04")).
		Str("sunrise", times.Sunrise.In(tz).Format("15:04")).
		Str("noon", times.Noon.In(tz).Format("15:04")).
		Str("sunset", times.Sunset.In(tz).Format("15:04")).
		Str("dusk", times.Dusk.In(tz).Format("15:04")).
		Msg("Astronomical times calculated")

	L.Push(result)
	return 1
}

