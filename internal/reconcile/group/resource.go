package group

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/dokzlo13/lightd/internal/reconcile"
	"github.com/dokzlo13/lightd/internal/state"
)

// Resource reconciles a single group's state.
type Resource struct {
	groupID string
	store   *state.TypedStore[Desired]
	actual  *ActualProvider
	applier Applier

	// Internal state populated by Load()
	desired        Desired
	desiredVersion int64
	actualState    Actual
}

// NewResource creates a new group resource.
func NewResource(
	groupID string,
	store *state.TypedStore[Desired],
	actual *ActualProvider,
	applier Applier,
) *Resource {
	return &Resource{
		groupID: groupID,
		store:   store,
		actual:  actual,
		applier: applier,
	}
}

// Key returns the resource key.
func (r *Resource) Key() reconcile.ResourceKey {
	return reconcile.ResourceKey{Kind: reconcile.KindGroup, ID: r.groupID}
}

// Load fetches both actual and desired state.
func (r *Resource) Load(ctx context.Context) error {
	var err error

	// Load desired state
	r.desired, r.desiredVersion, err = r.store.Get(r.groupID)
	if err != nil {
		return err
	}

	// Load actual state
	r.actualState, err = r.actual.Get(ctx, r.groupID)
	if err != nil {
		return err
	}

	return nil
}

// NeedsReconcile returns true if actual != desired.
func (r *Resource) NeedsReconcile() bool {
	action := DetermineAction(r.desired, r.actualState)
	return action != ActionNone
}

// ReconcileStep performs one transition step using the FSM.
func (r *Resource) ReconcileStep(ctx context.Context) (done bool, err error) {
	action := DetermineAction(r.desired, r.actualState)

	// Debug logging
	log.Debug().
		Str("group", r.groupID).
		Interface("desired", r.desired).
		Interface("actual", r.actualState).
		Str("action", action.String()).
		Msg("Group reconcile step")

	if action == ActionNone {
		return true, nil
	}

	return r.executeAction(ctx, action)
}

// executeAction executes the determined action.
func (r *Resource) executeAction(ctx context.Context, action Action) (done bool, err error) {
	switch action {
	case ActionTurnOnWithScene:
		if err := r.applier.TurnOnWithScene(ctx, r.groupID, r.desired.SceneName); err != nil {
			return false, err
		}
		r.actual.Update(r.groupID, Actual{
			AnyOn:            true,
			AllOn:            true,
			LastAppliedScene: r.desired.SceneName,
			AppliedAt:        time.Now(),
		})
		return true, nil

	case ActionTurnOnWithState:
		if err := r.applier.ApplyState(ctx, r.groupID, r.desired); err != nil {
			return false, err
		}
		r.actual.Update(r.groupID, Actual{
			AnyOn:            true,
			AllOn:            true,
			LastAppliedScene: "",
			AppliedAt:        time.Now(),
		})
		return true, nil

	case ActionTurnOff:
		if err := r.applier.TurnOff(ctx, r.groupID); err != nil {
			return false, err
		}
		r.actual.Update(r.groupID, Actual{
			AnyOn:            false,
			AllOn:            false,
			LastAppliedScene: "",
		})
		return true, nil

	case ActionApplyScene:
		if err := r.applier.ApplyScene(ctx, r.groupID, r.desired.SceneName); err != nil {
			return false, err
		}
		r.actual.Update(r.groupID, Actual{
			AnyOn:            true,
			AllOn:            r.actualState.AllOn,
			LastAppliedScene: r.desired.SceneName,
			AppliedAt:        time.Now(),
		})
		return true, nil

	case ActionApplyState:
		if err := r.applier.ApplyState(ctx, r.groupID, r.desired); err != nil {
			return false, err
		}
		return true, nil
	}

	return true, nil
}

// DesiredVersion returns the version of the desired state.
func (r *Resource) DesiredVersion() int64 {
	return r.desiredVersion
}
