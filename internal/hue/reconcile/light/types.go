// Package light provides the reconciliation resource for individual Hue lights.
package light

// Desired is the desired state for a light.
// Stored as JSON in the resource_state table.
type Desired struct {
	Power *bool     `json:"power,omitempty"` // nil = no opinion, true = on, false = off
	Bri   *uint8    `json:"bri,omitempty"`   // brightness (1-254)
	Hue   *uint16   `json:"hue,omitempty"`   // hue (0-65535)
	Sat   *uint8    `json:"sat,omitempty"`   // saturation (0-254)
	Xy    []float32 `json:"xy,omitempty"`    // CIE xy color coordinates
	Ct    *uint16   `json:"ct,omitempty"`    // color temperature in mirek (153-500)
}

// Actual is the actual state of a light (from Hue).
type Actual struct {
	On  bool
	Bri uint8
	Hue uint16
	Sat uint8
	Xy  []float32
	Ct  uint16
}

