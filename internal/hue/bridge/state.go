// Package bridge adapts the upstream OpenHue client to lightd's lighting model.
package bridge

import (
	"encoding/json"
	"fmt"
	"math"

	"github.com/google/uuid"
)

// State is a sparse command. Power is independent of appearance. Public values
// are percentage brightness, CIE xy, mirek and millisecond duration.
type State struct {
	Gradient   json.RawMessage `json:"gradient,omitempty"`
	On         *bool           `json:"on,omitempty"`
	Brightness *float64        `json:"brightness,omitempty"`
	Xy         []float32       `json:"xy,omitempty"`
	Ct         *uint16         `json:"ct,omitempty"`
	Duration   *int            `json:"duration,omitempty"`
	PowerMode  string          `json:"power_mode,omitempty"`
	Effect     string          `json:"effect,omitempty"`
	ColorMode  string          `json:"colormode,omitempty"`
	// Legacy payload fields are decoded only to detect unmigrated persisted state.
	// Reject them explicitly rather than silently misinterpreting old units.
	Bri            *uint8  `json:"bri,omitempty"`
	Hue            *uint16 `json:"hue,omitempty"`
	Sat            *uint8  `json:"sat,omitempty"`
	Alert          string  `json:"alert,omitempty"`
	TransitionTime *uint16 `json:"transitiontime,omitempty"`
}

type Light struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	State     State    `json:"state"`
	OwnerID   string   `json:"owner_id,omitempty"`
	Reachable *bool    `json:"reachable"`
	CTMin     int      `json:"ct_min,omitempty"`
	CTMax     int      `json:"ct_max,omitempty"`
	Effects   []string `json:"effects,omitempty"`
	Signals   []string `json:"signals,omitempty"`
}
type Group struct {
	ID        string   `json:"id"`
	GroupedID string   `json:"grouped_id"`
	Name      string   `json:"name"`
	Members   []string `json:"members"`
	State     State    `json:"state"`
	AnyOn     bool     `json:"any_on"`
	AllOn     bool     `json:"all_on"`
}
type Scene struct {
	ID      string           `json:"id"`
	Name    string           `json:"name"`
	GroupID string           `json:"group_id"`
	Members map[string]State `json:"members"`
}
type Snapshot struct {
	Lights map[string]State `json:"lights"`
}

func ValidateID(id string) error {
	if _, err := uuid.Parse(id); err != nil {
		return fmt.Errorf("Hue resource must be a native V2 UUID, got %q", id)
	}
	return nil
}
func (s State) Validate() error {
	if s.Bri != nil || s.Hue != nil || s.Sat != nil || s.TransitionTime != nil || s.Alert != "" {
		return fmt.Errorf("legacy Hue state fields are unsupported; use brightness, xy, duration and signal")
	}
	if s.Brightness != nil && (math.IsNaN(*s.Brightness) || math.IsInf(*s.Brightness, 0) || *s.Brightness < 0 || *s.Brightness > 100) {
		return fmt.Errorf("brightness must be finite and between 0 and 100")
	}
	if len(s.Xy) != 0 {
		if len(s.Xy) != 2 {
			return fmt.Errorf("xy must contain two coordinates")
		}
		for _, v := range s.Xy {
			if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) || v < 0 || v > 1 {
				return fmt.Errorf("xy coordinates must be finite and between 0 and 1")
			}
		}
	}
	if s.Ct != nil && (*s.Ct == 0) {
		return fmt.Errorf("mirek must be positive")
	}
	if s.Ct != nil && len(s.Xy) > 0 {
		return fmt.Errorf("choose color temperature or xy, not both")
	}
	if s.Duration != nil && (*s.Duration < 0 || *s.Duration > 3600000) {
		return fmt.Errorf("duration must be between 0 and 3600000 milliseconds")
	}
	switch s.PowerMode {
	case "", "preserve", "enforce", "next_on":
	default:
		return fmt.Errorf("unknown power mode %q", s.PowerMode)
	}
	if s.PowerMode == "preserve" && s.On != nil {
		return fmt.Errorf("preserve power cannot include on")
	}
	if s.PowerMode == "enforce" && s.On == nil {
		return fmt.Errorf("enforce power requires on")
	}
	return nil
}
func (s State) Appearance() State { s.On = nil; s.PowerMode = "preserve"; return s }
func (s State) HasAppearance() bool {
	return s.Brightness != nil || len(s.Xy) == 2 || s.Ct != nil || s.Effect != "" || len(s.Gradient) > 0
}

// HSVToXY converts hue in degrees and saturation percent at full value using
// sRGB gamma and the wide-gamut RGB-to-XYZ matrix used for Hue color control.
// Brightness remains an independent command; this does not invent power intent.
func HSVToXY(h, s float64) ([2]float64, error) {
	if math.IsNaN(h) || math.IsInf(h, 0) || math.IsNaN(s) || math.IsInf(s, 0) || h < 0 || h > 360 || s < 0 || s > 100 {
		return [2]float64{}, fmt.Errorf("HSV requires hue 0..360 degrees and saturation 0..100 percent")
	}
	h = math.Mod(h, 360) / 60
	s /= 100
	x := s * (1 - math.Abs(math.Mod(h, 2)-1))
	m := 1 - s
	var r, g, b float64
	switch int(h) {
	case 0:
		r, g = s, x
	case 1:
		r, g = x, s
	case 2:
		g, b = s, x
	case 3:
		g, b = x, s
	case 4:
		r, b = x, s
	default:
		r, b = s, x
	}
	r += m
	g += m
	b += m
	gamma := func(v float64) float64 {
		if v > 0.04045 {
			return math.Pow((v+0.055)/1.055, 2.4)
		}
		return v / 12.92
	}
	r = gamma(r)
	g = gamma(g)
	b = gamma(b)
	X := r*0.664511 + g*0.154324 + b*0.162028
	Y := r*0.283881 + g*0.668433 + b*0.047685
	Z := r*0.000088 + g*0.072310 + b*0.986039
	return [2]float64{X / (X + Y + Z), Y / (X + Y + Z)}, nil
}
