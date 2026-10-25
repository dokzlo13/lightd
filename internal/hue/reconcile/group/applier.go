package group

import (
	"context"
	"github.com/dokzlo13/lightd/internal/hue/bridge"
)

type Applier interface {
	ApplyDesired(context.Context, string, Desired) error
}
type HueApplier struct{ bridge *bridge.Client }

func NewHueApplier(b *bridge.Client, _ ...any) *HueApplier { return &HueApplier{b} }
func (a *HueApplier) ApplyDesired(ctx context.Context, id string, d Desired) error {
	return a.bridge.ApplyGroup(ctx, id, d.State, d.SceneName)
}
