// Package reconcile provides the reconciliation framework for making
// actual state match desired state.
package reconcile

import "context"

// Kind identifies a type of reconcilable resource.
type Kind string

// Resource kinds
const (
	KindGroup Kind = "group"
	KindLight Kind = "light"
)

// ResourceKey uniquely identifies a reconcilable resource.
type ResourceKey struct {
	Kind Kind
	ID   string
}

// Resource is the core abstraction for anything reconcilable.
// Each resource loads its own state internally and knows how to
// transition from actual to desired state.
type Resource interface {
	// Key returns unique identifier for this resource.
	Key() ResourceKey

	// Load fetches both actual and desired state into internal fields.
	Load(ctx context.Context) error

	// NeedsReconcile returns true if actual != desired (uses internal state).
	NeedsReconcile() bool

	// ReconcileStep performs one transition step. Returns:
	//   done=true:  converged or nothing to do
	//   done=false: call again (FSM multi-step)
	ReconcileStep(ctx context.Context) (done bool, err error)

	// DesiredVersion returns version of desired state (for dirty tracking).
	DesiredVersion() int64
}

// ResourceProvider creates and manages resources of a specific kind.
type ResourceProvider interface {
	// Kind returns the resource type this provider handles.
	Kind() Kind

	// ListDirty returns resources that have changed since last reconcile.
	ListDirty(ctx context.Context, lastVersions map[string]int64) ([]Resource, error)

	// Get returns a specific resource by ID.
	Get(ctx context.Context, id string) (Resource, error)
}
