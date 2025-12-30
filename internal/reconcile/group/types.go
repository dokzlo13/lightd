// Package group provides the reconciliation resource for Hue light groups.
package group

import (
	"time"
)

// Desired is the desired state for a group.
// Stored as JSON in the resource_state table.
type Desired struct {
	Power     *bool     `json:"power,omitempty"`      // nil = no opinion, true = on, false = off
	SceneName string    `json:"scene_name,omitempty"` // scene to apply when on
	Bri       *uint8    `json:"bri,omitempty"`        // brightness (1-254)
	Hue       *uint16   `json:"hue,omitempty"`        // hue (0-65535)
	Sat       *uint8    `json:"sat,omitempty"`        // saturation (0-254)
	Xy        []float32 `json:"xy,omitempty"`         // CIE xy color coordinates
	Ct        *uint16   `json:"ct,omitempty"`         // color temperature in mirek (153-500)
}

// Actual is the actual state of a group (from Hue + our tracking).
type Actual struct {
	AnyOn            bool
	AllOn            bool
	LastAppliedScene string    // scene name we last applied (empty if unknown)
	AppliedAt        time.Time // when we applied it
}

