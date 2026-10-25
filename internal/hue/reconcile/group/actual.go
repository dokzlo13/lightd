package group

import (
	"context"
	"github.com/dokzlo13/lightd/internal/hue/bridge"
)

type ActualProvider struct{ bridge *bridge.Client }

func NewActualProvider(b *bridge.Client) *ActualProvider { return &ActualProvider{b} }
func (p *ActualProvider) Get(ctx context.Context, id string) (Actual, error) {
	g, err := p.bridge.Group(ctx, id)
	if err != nil {
		return Actual{}, err
	}
	return Actual{State: g.State, AnyOn: g.AnyOn, AllOn: g.AllOn}, nil
}
func (p *ActualProvider) GetGroup(ctx context.Context, id string) (*bridge.Group, error) {
	g, err := p.bridge.Group(ctx, id)
	return &g, err
}
