package light

import (
	"context"
	"github.com/dokzlo13/lightd/internal/hue/bridge"
)

type Applier interface {
	ApplyState(context.Context, string, Desired) error
	TurnOff(context.Context, string) error
}
type HueApplier struct{ bridge *bridge.Client }

func NewHueApplier(b *bridge.Client) *HueApplier { return &HueApplier{b} }
func (a *HueApplier) ApplyState(ctx context.Context, id string, d Desired) error {
	return a.bridge.ApplyLight(ctx, id, d)
}
func (a *HueApplier) TurnOff(ctx context.Context, id string) error {
	v := false
	return a.bridge.ApplyLight(ctx, id, State{On: &v, PowerMode: "enforce"})
}
