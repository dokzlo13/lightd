package hue

// =============================================================================
// Application Types
// These are types used by the application layer, not tied to specific API versions
// =============================================================================

// StoredScene represents a scene stored in the state store
type StoredScene struct {
	SceneID   string `json:"scene_id"`
	SceneName string `json:"scene_name"`
	GroupID   string `json:"group_id"`
	SceneV2ID string `json:"scene_v2_id,omitempty"`
}
