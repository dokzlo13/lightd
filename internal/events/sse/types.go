// Package sse provides handler types for Hue SSE (Server-Sent Events).
package sse

import (
	"github.com/dokzlo13/lightd/internal/lua/modules/collect"
)

// LightResourceType represents the type of light resource from Hue SSE.
type LightResourceType string

const (
	LightResourceTypeLight        LightResourceType = "light"
	LightResourceTypeGroupedLight LightResourceType = "grouped_light"
)

// ButtonHandler is called when a button event occurs
type ButtonHandler struct {
	ResourceID       Matcher // Matches button resource ID ("*" for any, "id1|id2" for multiple)
	ButtonAction     Matcher // Matches button action ("*" for any, "short_release|long_release" etc)
	ActionName       string
	ActionArgs       map[string]any
	CollectorFactory *collect.CollectorFactory // nil = immediate
}

// ConnectivityHandler is called when connectivity changes
type ConnectivityHandler struct {
	DeviceID         Matcher // Matches device ID ("*" for any)
	Status           Matcher // Matches status ("connected", "disconnected", "*" for any)
	ActionName       string
	ActionArgs       map[string]any
	CollectorFactory *collect.CollectorFactory // nil = immediate
}

// RotaryHandler is called when a rotary event occurs
type RotaryHandler struct {
	ResourceID       Matcher // Matches rotary resource ID ("*" for any)
	ActionName       string
	ActionArgs       map[string]any
	CollectorFactory *collect.CollectorFactory // nil = immediate
}

// LightChangeHandler is called when a light state changes (brightness, power, color, etc.)
type LightChangeHandler struct {
	ResourceID       Matcher // Matches light resource ID
	ResourceType     Matcher // Matches LightResourceType
	ActionName       string
	ActionArgs       map[string]any
	CollectorFactory *collect.CollectorFactory // nil = immediate
}
