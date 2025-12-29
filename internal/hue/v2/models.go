package v2

// =============================================================================
// V2 API Types (CLIP API)
// These are not provided by huego, which only supports V1 API
// =============================================================================

// Light represents a Hue light (V2 API / CLIP)
type Light struct {
	ID       string `json:"id"`
	IDV1     string `json:"id_v1,omitempty"`
	Metadata struct {
		Name      string `json:"name"`
		Archetype string `json:"archetype"`
	} `json:"metadata"`
	On *struct {
		On bool `json:"on"`
	} `json:"on,omitempty"`
	Dimming *struct {
		Brightness float64 `json:"brightness"`
	} `json:"dimming,omitempty"`
	ColorTemperature *struct {
		Mirek      int  `json:"mirek"`
		MirekValid bool `json:"mirek_valid"`
	} `json:"color_temperature,omitempty"`
	Color *struct {
		XY struct {
			X float64 `json:"x"`
			Y float64 `json:"y"`
		} `json:"xy"`
	} `json:"color,omitempty"`
}

// Scene represents a Hue scene (V2 API / CLIP)
type Scene struct {
	ID       string `json:"id"`
	IDV1     string `json:"id_v1,omitempty"`
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Group struct {
		RID   string `json:"rid"`
		RType string `json:"rtype"`
	} `json:"group"`
	Actions []SceneAction `json:"actions"`
}

// SceneAction represents a scene action (V2 API)
type SceneAction struct {
	Target struct {
		RID   string `json:"rid"`
		RType string `json:"rtype"`
	} `json:"target"`
	Action ActionData `json:"action"`
}

// ActionData represents action data for a light (V2 API)
type ActionData struct {
	On *struct {
		On bool `json:"on"`
	} `json:"on,omitempty"`
	Dimming *struct {
		Brightness float64 `json:"brightness"`
	} `json:"dimming,omitempty"`
	ColorTemperature *struct {
		Mirek int `json:"mirek"`
	} `json:"color_temperature,omitempty"`
	Color *struct {
		XY struct {
			X float64 `json:"x"`
			Y float64 `json:"y"`
		} `json:"xy"`
	} `json:"color,omitempty"`
}

