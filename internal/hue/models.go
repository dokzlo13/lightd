package hue

// GroupState represents the state of a Hue group (v1 API)
type GroupState struct {
	AllOn bool `json:"all_on"`
	AnyOn bool `json:"any_on"`
}

// Group represents a Hue group (v1 API)
type Group struct {
	ID     string     `json:"id"`
	Name   string     `json:"name"`
	Lights []string   `json:"lights"`
	Type   string     `json:"type"`
	State  GroupState `json:"state"`
	Action struct {
		On  bool `json:"on"`
		Bri int  `json:"bri"`
	} `json:"action"`
}

// Scene represents a Hue scene (v1 API)
type Scene struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Group   string   `json:"group"`
	Lights  []string `json:"lights"`
	Type    string   `json:"type"`
	Version int      `json:"version"`
}

// SceneV2 represents a Hue scene (v2 API)
type SceneV2 struct {
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

// SceneAction represents a scene action
type SceneAction struct {
	Target struct {
		RID   string `json:"rid"`
		RType string `json:"rtype"`
	} `json:"target"`
	Action ActionData `json:"action"`
}

// ActionData represents action data for a light
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

// Light represents a Hue light (v2 API)
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

// ButtonEvent represents a button event from the event stream
type ButtonEvent struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Button *struct {
		ButtonReport struct {
			Event   string `json:"event"`
			Updated string `json:"updated"`
		} `json:"button_report"`
	} `json:"button,omitempty"`
}

// ConnectivityEvent represents a connectivity event
type ConnectivityEvent struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Status string `json:"status"`
}

// HueEvent represents an event from the Hue bridge
type HueEvent struct {
	ID   string                   `json:"id"`
	Data []map[string]interface{} `json:"data"`
}

// StoredScene represents a scene stored in the state store
type StoredScene struct {
	SceneID   string `json:"scene_id"`
	SceneName string `json:"scene_name"`
	GroupID   string `json:"group_id"`
	SceneV2ID string `json:"scene_v2_id,omitempty"`
}
