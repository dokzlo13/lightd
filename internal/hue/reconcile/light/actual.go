package light

import (
	"context"
	"github.com/dokzlo13/lightd/internal/hue/bridge"
)

type ActualProvider struct{ bridge *bridge.Client }

func NewActualProvider(b *bridge.Client) *ActualProvider { return &ActualProvider{b} }
func (p *ActualProvider) Get(ctx context.Context, id string) (Actual, error) {
	l, err := p.bridge.Light(ctx, id)
	if err != nil {
		return Actual{}, err
	}
	return Actual{State: l.State, Reachable: l.Reachable != nil && *l.Reachable}, nil
}
func (p *ActualProvider) GetLight(ctx context.Context, id string) (*bridge.Light, error) {
	l, err := p.bridge.Light(ctx, id)
	return &l, err
}
