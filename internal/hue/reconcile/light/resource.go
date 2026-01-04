package light

import (
	"context"

	"github.com/rs/zerolog/log"

	"github.com/dokzlo13/lightd/internal/hue/reconcile"
	"github.com/dokzlo13/lightd/internal/storage"
)

// Resource reconciles a single light's state.
type Resource struct {
	lightID string
	store   *storage.TypedStore[Desired]
	actual  *ActualProvider
	applier Applier

	// Internal state populated by Load()
	desired        Desired
	desiredVersion int64
	actualState    Actual
}

// NewResource creates a new light resource.
func NewResource(
	lightID string,
	store *storage.TypedStore[Desired],
	actual *ActualProvider,
	applier Applier,
) *Resource {
	return &Resource{
		lightID: lightID,
		store:   store,
		actual:  actual,
		applier: applier,
	}
}

// Key returns the resource key.
func (r *Resource) Key() reconcile.ResourceKey {
	return reconcile.ResourceKey{Kind: reconcile.KindLight, ID: r.lightID}
}

// Load fetches both actual and desired state.
func (r *Resource) Load(ctx context.Context) error {
	var err error

	// Load desired state
	r.desired, r.desiredVersion, err = r.store.Get(r.lightID)
	if err != nil {
		return err
	}

	// Load actual state from bridge
	r.actualState, err = r.actual.Get(ctx, r.lightID)
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

	// Debug logging with full state info
	log.Debug().
		Str("light", r.lightID).
		Interface("desired", r.desired).
		Interface("actual", r.actualState).
		Bool("reachable", r.actualState.Reachable).
		Str("action", action.String()).
		Msg("Light reconcile step")

	if action == ActionNone {
		return true, nil
	}

	return r.executeAction(ctx, action)
}

// executeAction executes the determined action.
func (r *Resource) executeAction(ctx context.Context, action Action) (done bool, err error) {
	switch action {
	case ActionTurnOnWithState:
		if err := r.applier.ApplyState(ctx, r.lightID, r.desired); err != nil {
			return false, err
		}
		return true, nil

	case ActionTurnOff:
		if err := r.applier.TurnOff(ctx, r.lightID); err != nil {
			return false, err
		}
		return true, nil

	case ActionApplyState:
		if err := r.applier.ApplyState(ctx, r.lightID, r.desired); err != nil {
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

// GetDesired returns the current desired state (for external inspection).
func (r *Resource) GetDesired() Desired {
	return r.desired
}

// GetActual returns the current actual state (for external inspection).
func (r *Resource) GetActual() Actual {
	return r.actualState
}
