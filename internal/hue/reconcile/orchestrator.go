package reconcile

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

type RetryStatus struct {
	Version  int64     `json:"version"`
	Attempts int       `json:"attempts"`
	Next     time.Time `json:"next_attempt"`
	Error    string    `json:"error"`
}
type Status struct {
	Running      bool                   `json:"running"`
	Pending      int                    `json:"pending"`
	Retrying     map[string]RetryStatus `json:"retrying"`
	Acknowledged map[string]int64       `json:"acknowledged_versions"`
}

// Orchestrator retries failed persisted intent independently of periodic drift
// enforcement. Retry delay is bounded (1s..60s); no worker sleeps during backoff.
type Orchestrator struct {
	providers        map[Kind]ResourceProvider
	mu               sync.Mutex
	lastVersions     map[ResourceKey]int64
	pending          map[ResourceKey]struct{}
	retries          map[ResourceKey]RetryStatus
	trigger          chan struct{}
	periodicInterval time.Duration
	debounceMs       int
	running          bool
}

func NewOrchestrator(interval time.Duration, debounceMs int, _ float64) *Orchestrator {
	return &Orchestrator{providers: map[Kind]ResourceProvider{}, lastVersions: map[ResourceKey]int64{}, pending: map[ResourceKey]struct{}{}, retries: map[ResourceKey]RetryStatus{}, trigger: make(chan struct{}, 1), periodicInterval: interval, debounceMs: debounceMs}
}
func (o *Orchestrator) Register(p ResourceProvider) { o.providers[p.Kind()] = p }
func (o *Orchestrator) Trigger() {
	select {
	case o.trigger <- struct{}{}:
	default:
	}
}
func (o *Orchestrator) TriggerResource(k ResourceKey) {
	o.mu.Lock()
	o.pending[k] = struct{}{}
	o.mu.Unlock()
	o.Trigger()
}
func (o *Orchestrator) TriggerGroup(id string) { o.TriggerResource(ResourceKey{KindGroup, id}) }
func (o *Orchestrator) TriggerAll(ctx context.Context) {
	for kind, p := range o.providers {
		ids, err := p.ListAllIDs(ctx)
		if err != nil {
			log.Error().Err(err).Msg("Cannot list desired Hue resources")
			continue
		}
		o.mu.Lock()
		for _, id := range ids {
			o.pending[ResourceKey{kind, id}] = struct{}{}
		}
		o.mu.Unlock()
	}
	o.Trigger()
}
func (o *Orchestrator) Status() Status {
	o.mu.Lock()
	defer o.mu.Unlock()
	s := Status{Running: o.running, Pending: len(o.pending), Retrying: map[string]RetryStatus{}, Acknowledged: map[string]int64{}}
	for k, v := range o.retries {
		s.Retrying[string(k.Kind)+":"+k.ID] = v
	}
	for k, v := range o.lastVersions {
		s.Acknowledged[string(k.Kind)+":"+k.ID] = v
	}
	return s
}
func (o *Orchestrator) Run(ctx context.Context) error {
	o.mu.Lock()
	o.running = true
	o.mu.Unlock()
	defer func() { o.mu.Lock(); o.running = false; o.mu.Unlock() }()
	retryTicker := time.NewTicker(time.Second)
	defer retryTicker.Stop()
	var periodic *time.Ticker
	var periodicC <-chan time.Time
	if o.periodicInterval > 0 {
		periodic = time.NewTicker(o.periodicInterval)
		defer periodic.Stop()
		periodicC = periodic.C
	}
	// Rebuild work from persisted desired state on each boot. Persisted manual
	// fences in the bridge prevent stale intent from winning after restart.
	o.reconcileAll(ctx)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-o.trigger:
			if o.debounceMs > 0 {
				timer := time.NewTimer(time.Duration(o.debounceMs) * time.Millisecond)
				select {
				case <-ctx.Done():
					timer.Stop()
					return nil
				case <-timer.C:
				}
			}
			o.reconcileAll(ctx)
		case <-retryTicker.C:
			o.mu.Lock()
			hasRetries := len(o.retries) > 0
			o.mu.Unlock()
			if hasRetries {
				o.reconcileAll(ctx)
			}
		case <-periodicC:
			o.reconcileAll(ctx)
		}
	}
}
func (o *Orchestrator) reconcileAll(ctx context.Context) {
	o.mu.Lock()
	pending := make(map[ResourceKey]struct{}, len(o.pending))
	for k := range o.pending {
		pending[k] = struct{}{}
	}
	last := map[Kind]map[string]int64{}
	for k, v := range o.lastVersions {
		if last[k.Kind] == nil {
			last[k.Kind] = map[string]int64{}
		}
		last[k.Kind][k.ID] = v
	}
	o.mu.Unlock()
	kinds := make([]string, 0, len(o.providers))
	for k := range o.providers {
		kinds = append(kinds, string(k))
	}
	sort.Strings(kinds)
	for _, kindString := range kinds {
		kind := Kind(kindString)
		p := o.providers[kind]
		dirty, err := p.ListDirty(ctx, last[kind])
		if err != nil {
			log.Error().Err(err).Str("kind", kindString).Msg("Cannot list dirty Hue resources")
			o.mu.Lock()
			for k := range pending {
				if k.Kind == kind {
					o.failLocked(k, 0, err)
				}
			}
			o.mu.Unlock()
			continue
		}
		seen := map[string]bool{}
		for _, r := range dirty {
			seen[r.Key().ID] = true
		}
		for k := range pending {
			if k.Kind == kind && !seen[k.ID] {
				r, err := p.Get(ctx, k.ID)
				if err != nil {
					o.mu.Lock()
					o.failLocked(k, 0, err)
					o.mu.Unlock()
					continue
				}
				dirty = append(dirty, r)
			}
		}
		sort.Slice(dirty, func(i, j int) bool { return dirty[i].Key().ID < dirty[j].Key().ID })
		for _, r := range dirty {
			if ctx.Err() != nil {
				return
			}
			k := r.Key()
			o.mu.Lock()
			retry, hasRetry := o.retries[k]
			o.mu.Unlock()
			if hasRetry && retry.Version == r.DesiredVersion() && time.Now().Before(retry.Next) {
				continue
			}
			err := o.reconcileOne(ctx, r)
			o.mu.Lock()
			if err != nil {
				o.pending[k] = struct{}{}
				o.failLocked(k, r.DesiredVersion(), err)
			} else {
				o.lastVersions[k] = r.DesiredVersion()
				delete(o.pending, k)
				delete(o.retries, k)
			}
			o.mu.Unlock()
			if err != nil {
				log.Warn().Err(err).Str("kind", string(k.Kind)).Str("id", k.ID).Msg("Hue intent pending retry")
			}
		}
	}
}
func (o *Orchestrator) failLocked(k ResourceKey, version int64, err error) {
	r := o.retries[k]
	if r.Version != version {
		r = RetryStatus{Version: version}
	}
	r.Attempts++
	r.Error = err.Error()
	shift := r.Attempts - 1
	if shift > 6 {
		shift = 6
	}
	delay := time.Second * time.Duration(1<<shift)
	if delay > time.Minute {
		delay = time.Minute
	}
	r.Next = time.Now().Add(delay)
	o.retries[k] = r
	o.pending[k] = struct{}{}
}
func (o *Orchestrator) reconcileOne(ctx context.Context, r Resource) error {
	if err := r.Load(ctx); err != nil {
		return err
	}
	if !r.NeedsReconcile() {
		return nil
	}
	_, err := r.ReconcileStep(ctx)
	return err
}
