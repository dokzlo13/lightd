package hue

import "github.com/dokzlo13/lightd/internal/hue/bridge"

// SceneIndex remains an internal construction handle. The bridge owns an
// atomically refreshed native V2 catalog and resolves ambiguous names safely.
type SceneIndex struct{ Client *bridge.Client }

func NewSceneIndex() *SceneIndex { return &SceneIndex{} }
func (s *SceneIndex) Count() int {
	if s.Client == nil {
		return 0
	}
	return len(s.Client.Scenes())
}
