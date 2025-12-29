// Package reconcile provides the reconciliation loop that makes Hue match desired state.
package reconcile

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"golang.org/x/time/rate"

	"github.com/dokzlo13/lightd/internal/hue"
	"github.com/dokzlo13/lightd/internal/state"
)

// ActualState provides access to current Hue state (cached with fetch-on-stale).
type ActualState interface {
	Group(ctx context.Context, id string) (*hue.GroupState, error)
	FetchAndCache(ctx context.Context, id string) (*hue.GroupState, error)
	UpdateGroupState(id string, state hue.GroupState)
}

// Reconciler makes Hue match desired state
type Reconciler struct {
	hueClient    *hue.Client
	actualState  ActualState
	desiredStore *state.DesiredStore

	// Configuration
	periodicInterval time.Duration

	// Rate limiting for Hue API calls
	limiter *rate.Limiter

	// Per-group tracking
	mu          sync.Mutex
	lastVersion map[string]int64 // group -> last reconciled version
	pending     map[string]bool  // group -> needs reconcile

	// Channel to trigger reconciliation
	trigger chan struct{}
}

// New creates a new Reconciler
func New(hueClient *hue.Client, actualState ActualState, desiredStore *state.DesiredStore, periodicInterval time.Duration, rateLimitRPS float64) *Reconciler {
	if periodicInterval == 0 {
		periodicInterval = 5 * time.Minute
	}
	if rateLimitRPS == 0 {
		rateLimitRPS = 10.0
	}

	// Convert RPS to rate.Limiter format
	limiter := rate.NewLimiter(rate.Limit(rateLimitRPS), int(rateLimitRPS))

	return &Reconciler{
		hueClient:        hueClient,
		actualState:      actualState,
		desiredStore:     desiredStore,
		periodicInterval: periodicInterval,
		limiter:          limiter,
		lastVersion:      make(map[string]int64),
		pending:          make(map[string]bool),
		trigger:          make(chan struct{}, 1),
	}
}

// Trigger marks all dirty groups for reconciliation
func (r *Reconciler) Trigger() {
	select {
	case r.trigger <- struct{}{}:
	default:
		// Already triggered
	}
}

// TriggerGroup marks a specific group for reconciliation
func (r *Reconciler) TriggerGroup(groupID string) {
	r.mu.Lock()
	r.pending[groupID] = true
	r.mu.Unlock()
	r.Trigger()
}

// Run starts the reconciliation loop
func (r *Reconciler) Run(ctx context.Context) error {
	log.Info().Dur("periodic_interval", r.periodicInterval).Msg("Reconciler started")

	// Periodic reconcile
	ticker := time.NewTicker(r.periodicInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("Reconciler stopping")
			return nil

		case <-r.trigger:
			r.reconcileAll(ctx)

		case <-ticker.C:
			r.reconcileAll(ctx)
		}
	}
}

func (r *Reconciler) reconcileAll(ctx context.Context) {
	// Get all dirty groups
	dirty, err := r.desiredStore.GetDirtyGroups(r.lastVersion)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get dirty groups")
		return
	}

	// Add pending groups
	r.mu.Lock()
	for groupID := range r.pending {
		found := false
		for _, d := range dirty {
			if d == groupID {
				found = true
				break
			}
		}
		if !found {
			dirty = append(dirty, groupID)
		}
	}
	r.pending = make(map[string]bool)
	r.mu.Unlock()

	for _, groupID := range dirty {
		if err := r.reconcileGroup(ctx, groupID); err != nil {
			log.Error().Err(err).Str("group", groupID).Msg("Failed to reconcile group")
		}
	}
}

func (r *Reconciler) reconcileGroup(ctx context.Context, groupID string) error {
	for {
		// Read desired state + version atomically
		desired, version := r.desiredStore.GetWithVersion(groupID)

		// Get actual state (cached with fetch-on-stale)
		actual, err := r.actualState.Group(ctx, groupID)
		if err != nil {
			return err
		}

		// Compute diff and apply
		if err := r.applyDiff(ctx, groupID, desired, actual); err != nil {
			return err
		}

		// Check if version changed during apply
		_, currentVersion := r.desiredStore.GetWithVersion(groupID)
		if currentVersion == version {
			r.mu.Lock()
			r.lastVersion[groupID] = version
			r.mu.Unlock()
			return nil // Done, no changes during reconcile
		}
		// Version changed, loop again
		log.Debug().Str("group", groupID).Msg("State changed during reconcile, looping")
	}
}

func (r *Reconciler) applyDiff(ctx context.Context, groupID string, desired *state.DesiredState, actual *hue.GroupState) error {
	power := desired.Power()
	bank := desired.Bank()

	// Determine actions based on behavioral specs
	if power != nil && *power {
		// Desired: ON
		if !actual.AnyOn {
			// Actual: OFF -> Turn on with scene
			if bank == "" {
				log.Warn().Str("group", groupID).Msg("Cannot turn on: bank not set")
				return nil
			}
			return r.turnOnWithScene(ctx, groupID, bank)
		} else if bank != "" {
			// Actual: ON -> Apply scene (bank changed while on)
			return r.applyScene(ctx, groupID, bank)
		}
	} else if power != nil && !*power {
		// Desired: OFF
		if actual.AnyOn {
			// Actual: ON -> Turn off
			return r.turnOff(ctx, groupID)
		}
	} else {
		// Desired power: unset
		if bank != "" && actual.AnyOn {
			// Bank changed while on -> apply scene
			return r.applyScene(ctx, groupID, bank)
		}
		// Bank changed while off -> no-op (store only)
	}

	return nil
}

func (r *Reconciler) turnOnWithScene(ctx context.Context, groupID, sceneName string) error {
	// Wait for rate limiter
	if err := r.limiter.Wait(ctx); err != nil {
		return err
	}

	log.Info().
		Str("group", groupID).
		Str("scene", sceneName).
		Msg("Turning on with scene")

	// Find scene by name
	scene, err := r.hueClient.FindSceneByName(ctx, sceneName, groupID)
	if err != nil {
		return err
	}

	// Activate scene (which also turns on the lights)
	if err := r.hueClient.ActivateScene(ctx, groupID, scene.ID); err != nil {
		return err
	}

	// Update cache to reflect the change immediately
	r.actualState.UpdateGroupState(groupID, hue.GroupState{AllOn: true, AnyOn: true})
	return nil
}

func (r *Reconciler) applyScene(ctx context.Context, groupID, sceneName string) error {
	if err := r.limiter.Wait(ctx); err != nil {
		return err
	}

	log.Info().
		Str("group", groupID).
		Str("scene", sceneName).
		Msg("Applying scene")

	scene, err := r.hueClient.FindSceneByName(ctx, sceneName, groupID)
	if err != nil {
		return err
	}

	if err := r.hueClient.ActivateScene(ctx, groupID, scene.ID); err != nil {
		return err
	}

	// Update cache - applying a scene turns lights on
	r.actualState.UpdateGroupState(groupID, hue.GroupState{AllOn: true, AnyOn: true})
	return nil
}

func (r *Reconciler) turnOff(ctx context.Context, groupID string) error {
	if err := r.limiter.Wait(ctx); err != nil {
		return err
	}

	log.Info().
		Str("group", groupID).
		Msg("Turning off")

	if err := r.hueClient.SetGroupAction(ctx, groupID, map[string]interface{}{"on": false}); err != nil {
		return err
	}

	// Update cache to reflect the change immediately
	r.actualState.UpdateGroupState(groupID, hue.GroupState{AllOn: false, AnyOn: false})
	return nil
}
