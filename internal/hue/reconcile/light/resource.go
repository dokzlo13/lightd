package light

import (
	"context"

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

	// Load actual state
	r.actualState, err = r.actual.Get(ctx, r.lightID)
	if err != nil {
		return err
	}

	return nil
}

// NeedsReconcile returns true if actual != desired.
func (r *Resource) NeedsReconcile() bool {
	d := r.desired
	a := r.actualState

	// Power transitions
	if d.Power != nil {
		if *d.Power && !a.On {
			return true // OFF -> ON
		}
		if !*d.Power && a.On {
			return true // ON -> OFF
		}
	}

	// Only check other properties if light is on (or being turned on)
	if a.On || (d.Power != nil && *d.Power) {
		if d.Bri != nil && *d.Bri != a.Bri {
			return true
		}
		if d.Hue != nil && *d.Hue != a.Hue {
			return true
		}
		if d.Sat != nil && *d.Sat != a.Sat {
			return true
		}
		if d.Ct != nil && *d.Ct != a.Ct {
			return true
		}
		if d.Xy != nil && !xyEqual(d.Xy, a.Xy) {
			return true
		}
	}

	return false
}

// ReconcileStep performs one transition step.
func (r *Resource) ReconcileStep(ctx context.Context) (done bool, err error) {
	d := r.desired
	a := r.actualState

	switch {
	case d.Power != nil && *d.Power && !a.On:
		// OFF -> ON
		// Apply all desired state at once (power + properties)
		if err := r.applier.Apply(ctx, r.lightID, d); err != nil {
			return false, err
		}
		return true, nil

	case d.Power != nil && !*d.Power && a.On:
		// ON -> OFF
		if err := r.applier.TurnOff(ctx, r.lightID); err != nil {
			return false, err
		}
		return true, nil

	case a.On:
		// Light is on, apply property changes
		if r.needsPropertyUpdate() {
			if err := r.applier.Apply(ctx, r.lightID, d); err != nil {
				return false, err
			}
		}
		return true, nil
	}

	return true, nil // Nothing to do
}

// needsPropertyUpdate checks if any property needs updating.
func (r *Resource) needsPropertyUpdate() bool {
	d := r.desired
	a := r.actualState

	if d.Bri != nil && *d.Bri != a.Bri {
		return true
	}
	if d.Hue != nil && *d.Hue != a.Hue {
		return true
	}
	if d.Sat != nil && *d.Sat != a.Sat {
		return true
	}
	if d.Ct != nil && *d.Ct != a.Ct {
		return true
	}
	if d.Xy != nil && !xyEqual(d.Xy, a.Xy) {
		return true
	}
	return false
}

// DesiredVersion returns the version of the desired state.
func (r *Resource) DesiredVersion() int64 {
	return r.desiredVersion
}

// xyEqual compares two XY coordinate slices.
func xyEqual(a, b []float32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		// Use tolerance for float comparison
		if abs32(a[i]-b[i]) > 0.001 {
			return false
		}
	}
	return true
}

func abs32(x float32) float32 {
	if x < 0 {
		return -x
	}
	return x
}

