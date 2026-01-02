// Package actions provides the action registry and invocation system.
package actions

import (
	"context"

	"github.com/dokzlo13/lightd/internal/hue/reconcile/group"
	"github.com/dokzlo13/lightd/internal/storage"
)

// ActualState provides read-only access to the actual Hue state
type ActualState interface {
	Get(ctx context.Context, groupID string) (group.Actual, error)
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
	desired    *storage.TypedStore[group.Desired]
	reconciler Reconciler
	runAction  func(name string, args map[string]any) error
}

// NewContext creates a new ActionContext
func NewContext(
	ctx context.Context,
	actual ActualState,
	desired *storage.TypedStore[group.Desired],
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

// Desired returns the desired state store
func (c *Context) Desired() *storage.TypedStore[group.Desired] {
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

// --- Convenience methods for common operations ---

// SetPower sets the desired power state for a group
func (c *Context) SetPower(groupID string, on bool) error {
	return c.desired.Update(groupID, func(current group.Desired) group.Desired {
		current.Power = &on
		return current
	})
}

// SetScene sets the desired scene for a group
func (c *Context) SetScene(groupID string, sceneName string) error {
	return c.desired.Update(groupID, func(current group.Desired) group.Desired {
		current.SceneName = sceneName
		return current
	})
}

// GetDesiredState returns the current desired state for a group
func (c *Context) GetDesiredState(groupID string) (group.Desired, error) {
	state, _, err := c.desired.Get(groupID)
	return state, err
}

// GetActualState returns the current actual state for a group
func (c *Context) GetActualState(groupID string) (group.Actual, error) {
	return c.actual.Get(c.ctx, groupID)
}

// HasScene returns true if the group has a scene set
func (c *Context) HasScene(groupID string) bool {
	state, _, err := c.desired.Get(groupID)
	if err != nil {
		return false
	}
	return state.SceneName != ""
}

// GetScene returns the scene name for a group, or empty if not set
func (c *Context) GetScene(groupID string) string {
	state, _, err := c.desired.Get(groupID)
	if err != nil {
		return ""
	}
	return state.SceneName
}
