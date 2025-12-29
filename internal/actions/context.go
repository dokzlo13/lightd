// Package actions provides the action registry and invocation system.
package actions

import (
	"context"

	"github.com/amimof/huego"

	"github.com/dokzlo13/lightd/internal/state"
)

// ActualState provides read-only access to the actual Hue state
type ActualState interface {
	Group(ctx context.Context, id string) (*huego.GroupState, error)
}

// DesiredState provides access to the desired state store
type DesiredState interface {
	SetBank(groupID string, sceneName string) error
	SetPower(groupID string, on bool) error
	GetBank(groupID string) string
	HasBank(groupID string) bool
}

// Reconciler triggers reconciliation
type Reconciler interface {
	Trigger()
	TriggerGroup(groupID string)
}

// Context is the capability interface provided to actions
// It exposes stable methods, not raw pointers
type Context struct {
	ctx        context.Context // Go context for cancellation/timeout
	actual     ActualState
	desired    *state.DesiredStore
	reconciler Reconciler
	runAction  func(name string, args map[string]any) error
}

// NewContext creates a new ActionContext
func NewContext(
	ctx context.Context,
	actual ActualState,
	desired *state.DesiredStore,
	reconciler Reconciler,
	runAction func(name string, args map[string]any) error,
) *Context {
	return &Context{
		ctx:        ctx,
		actual:     actual,
		desired:    desired,
		reconciler: reconciler,
		runAction:  runAction,
	}
}

// Context returns the Go context for cancellation
func (c *Context) Ctx() context.Context {
	return c.ctx
}

// Actual returns the actual state accessor
func (c *Context) Actual() ActualState {
	return c.actual
}

// Desired returns the desired state accessor
func (c *Context) Desired() DesiredState {
	return c.desired
}

// Reconcile triggers reconciliation
func (c *Context) Reconcile() {
	if c.reconciler != nil {
		c.reconciler.Trigger()
	}
}

// RunAction runs another action by name (for composition)
func (c *Context) RunAction(name string, args map[string]any) error {
	if c.runAction != nil {
		return c.runAction(name, args)
	}
	return nil
}
