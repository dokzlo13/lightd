package reconcile

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"golang.org/x/time/rate"
)

// Orchestrator coordinates reconciliation across all resource types.
// It's domain-agnostic - all resource-specific logic lives in providers.
type Orchestrator struct {
	providers map[Kind]ResourceProvider
	limiter   *rate.Limiter

	mu           sync.Mutex
	lastVersions map[ResourceKey]int64    // tracks last reconciled version per resource
	pending      map[ResourceKey]struct{} // manual triggers awaiting reconcile
	trigger      chan struct{}

	// Configuration
	periodicInterval time.Duration
	debounceMs       int
}

// NewOrchestrator creates a new reconciliation orchestrator.
func NewOrchestrator(periodicInterval time.Duration, debounceMs int, rateLimitRPS float64) *Orchestrator {
	// periodicInterval=0 means disabled (no default fallback)
	if rateLimitRPS == 0 {
		rateLimitRPS = 10.0
	}

	limiter := rate.NewLimiter(rate.Limit(rateLimitRPS), int(rateLimitRPS))

	return &Orchestrator{
		providers:        make(map[Kind]ResourceProvider),
		limiter:          limiter,
		lastVersions:     make(map[ResourceKey]int64),
		pending:          make(map[ResourceKey]struct{}),
		trigger:          make(chan struct{}, 1),
		periodicInterval: periodicInterval,
		debounceMs:       debounceMs,
	}
}

// Register adds a resource provider.
func (o *Orchestrator) Register(provider ResourceProvider) {
	o.providers[provider.Kind()] = provider
}

// Trigger signals that reconciliation should run.
func (o *Orchestrator) Trigger() {
	select {
	case o.trigger <- struct{}{}:
	default:
		// Already triggered
	}
}

// TriggerResource marks a specific resource for reconciliation.
func (o *Orchestrator) TriggerResource(key ResourceKey) {
	o.mu.Lock()
	o.pending[key] = struct{}{}
	o.mu.Unlock()
	o.Trigger()
}

// TriggerGroup is a convenience method for triggering group reconciliation.
// Implements the Reconciler interface used by actions.
func (o *Orchestrator) TriggerGroup(groupID string) {
	o.TriggerResource(ResourceKey{Kind: KindGroup, ID: groupID})
}

// TriggerAll marks ALL resources with desired state for reconciliation.
// This is used to enforce desired state against external changes.
func (o *Orchestrator) TriggerAll(ctx context.Context) {
	o.mu.Lock()
	defer o.mu.Unlock()

	for kind, provider := range o.providers {
		// Clear any cached state so reconciler will re-apply
		provider.ClearCaches()

		ids, err := provider.ListAllIDs(ctx)
		if err != nil {
			log.Error().Err(err).Str("kind", string(kind)).Msg("ListAllIDs failed")
			continue
		}
		for _, id := range ids {
			o.pending[ResourceKey{Kind: kind, ID: id}] = struct{}{}
		}
		log.Debug().Str("kind", string(kind)).Int("count", len(ids)).Msg("Marked all resources as pending")
	}

	// Trigger reconciliation
	select {
	case o.trigger <- struct{}{}:
	default:
	}
}

// Run starts the reconciliation loop.
func (o *Orchestrator) Run(ctx context.Context) error {
	log.Info().
		Dur("periodic_interval", o.periodicInterval).
		Int("debounce_ms", o.debounceMs).
		Msg("Orchestrator started")

	// Set up periodic ticker (nil if disabled)
	var ticker *time.Ticker
	var tickerC <-chan time.Time
	if o.periodicInterval > 0 {
		ticker = time.NewTicker(o.periodicInterval)
		tickerC = ticker.C
		defer ticker.Stop()
	}

	// Debounce timer (nil until first trigger)
	var debounceTimer *time.Timer
	var debounceC <-chan time.Time

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("Orchestrator stopping")
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			return nil

		case <-o.trigger:
			if o.debounceMs > 0 {
				// Start or reset debounce timer
				if debounceTimer == nil {
					debounceTimer = time.NewTimer(time.Duration(o.debounceMs) * time.Millisecond)
					debounceC = debounceTimer.C
				} else {
					// Reset timer (drain channel first if needed)
					if !debounceTimer.Stop() {
						select {
						case <-debounceTimer.C:
						default:
						}
					}
					debounceTimer.Reset(time.Duration(o.debounceMs) * time.Millisecond)
				}
			} else {
				// No debounce, run immediately
				o.reconcileAll(ctx)
			}

		case <-debounceC:
			// Debounce period elapsed, run reconciliation
			o.reconcileAll(ctx)

		case <-tickerC:
			// Periodic reconciliation
			o.reconcileAll(ctx)
		}
	}
}

func (o *Orchestrator) reconcileAll(ctx context.Context) {
	// 1. Snapshot and clear pending (under lock, once)
	log.Debug().Msg("Reconciliation started")
	o.mu.Lock()
	pendingSnapshot := o.pending
	o.pending = make(map[ResourceKey]struct{})
	// log.Debug().Int("pending_count", len(pendingSnapshot)).Msg("snapshotted pending resources")

	// Build lastVersions per kind for dirty queries
	lastByKind := make(map[Kind]map[string]int64)
	for key, ver := range o.lastVersions {
		if lastByKind[key.Kind] == nil {
			lastByKind[key.Kind] = make(map[string]int64)
		}
		lastByKind[key.Kind][key.ID] = ver
	}
	// log.Debug().Int("tracked_versions", len(o.lastVersions)).Msg("built lastVersions map")
	o.mu.Unlock()

	// 2. For each provider, get dirty + pending resources
	for kind, provider := range o.providers {
		// log.Debug().Str("kind", string(kind)).Msg("processing kind")

		// Get dirty from store
		dirty, err := provider.ListDirty(ctx, lastByKind[kind])
		if err != nil {
			log.Error().Err(err).Str("kind", string(kind)).Msg("ListDirty failed")
			continue
		}
		log.Debug().Str("kind", string(kind)).Int("dirty_count", len(dirty)).Msg("retrieved dirty resources")

		// Track which IDs we already have
		seen := make(map[string]bool)
		for _, r := range dirty {
			seen[r.Key().ID] = true
		}

		// Merge in pending for this kind
		pendingForKind := 0
		for key := range pendingSnapshot {
			if key.Kind == kind && !seen[key.ID] {
				r, err := provider.Get(ctx, key.ID)
				if err != nil {
					log.Error().Err(err).Str("id", key.ID).Msg("Get pending resource failed")
					continue
				}
				dirty = append(dirty, r)
				pendingForKind++
			}
		}
		if pendingForKind > 0 {
			log.Debug().Str("kind", string(kind)).Int("merged_pending", pendingForKind).Int("total", len(dirty)).Msg("merged pending resources")
		}

		// 3. Reconcile each resource
		log.Debug().Str("kind", string(kind)).Int("total_resources", len(dirty)).Msg("starting reconciliation")
		successCount := 0
		for _, r := range dirty {
			log.Debug().Str("kind", string(kind)).Str("id", r.Key().ID).Int64("version", r.DesiredVersion()).Msg("reconciling resource")

			if err := o.reconcileOne(ctx, r); err != nil {
				log.Error().Err(err).
					Str("kind", string(kind)).
					Str("id", r.Key().ID).
					Msg("Reconcile failed")
				continue
			}

			// Update last version on success
			o.mu.Lock()
			o.lastVersions[r.Key()] = r.DesiredVersion()
			o.mu.Unlock()

			successCount++
			log.Debug().Str("kind", string(kind)).Str("id", r.Key().ID).Int64("version", r.DesiredVersion()).Msg("resource reconciled successfully")
		}

		log.Debug().Str("kind", string(kind)).Int("success", successCount).Int("total", len(dirty)).Msg("completed reconciliation for kind")
	}

	log.Debug().Msg("reconcileAll completed")
}

func (o *Orchestrator) reconcileOne(ctx context.Context, r Resource) error {
	for {
		// Rate limit
		if err := o.limiter.Wait(ctx); err != nil {
			return err
		}

		// Load current state
		if err := r.Load(ctx); err != nil {
			log.Error().Err(err).Str("id", r.Key().ID).Msg("Load failed")
			return err
		}

		// Check if reconciliation needed
		if !r.NeedsReconcile() {
			return nil
		}

		// Perform one reconciliation step
		done, err := r.ReconcileStep(ctx)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
		// FSM needs another step, loop again
	}
}
