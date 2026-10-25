package app

import (
	"context"
	"database/sql"
	"sync"

	"github.com/dokzlo13/lightd/internal/config"
	"github.com/dokzlo13/lightd/internal/events"
	"github.com/dokzlo13/lightd/internal/hue"
	"github.com/dokzlo13/lightd/internal/hue/reconcile"
	"github.com/dokzlo13/lightd/internal/hue/reconcile/group"
	"github.com/dokzlo13/lightd/internal/hue/reconcile/light"
	v2 "github.com/dokzlo13/lightd/internal/hue/v2"
	"github.com/dokzlo13/lightd/internal/storage"
)

type HueService struct {
	cfg           *config.Config
	Client        *hue.Client
	SceneIndex    *hue.SceneIndex
	EventStream   *v2.EventStream
	Orchestrator  *reconcile.Orchestrator
	Bus           *events.Bus
	Stores        *hue.StoreRegistry
	GroupProvider *group.Provider
	LightProvider *light.Provider
	cancel        context.CancelFunc
	wg            sync.WaitGroup
}

func NewHueService(cfg *config.Config, db *sql.DB, store *storage.Store) (*HueService, error) {
	c, err := hue.NewClient(cfg.Hue.Bridge, cfg.Hue.Token, cfg.Hue.GetTimeout())
	if err != nil {
		return nil, err
	}
	if err := c.SetRateLimit(cfg.Reconciler.GetRateLimitRPS()); err != nil {
		return nil, err
	}
	if err := c.SetDB(db); err != nil {
		return nil, err
	}
	stores := hue.NewStoreRegistry(store)
	stores.SetClearAuthority(c.ClearAuthority)
	scenes := &hue.SceneIndex{Client: c}
	gp := group.NewProvider(stores.Groups(), group.NewActualProvider(c), group.NewHueApplier(c))
	lp := light.NewProvider(stores.Lights(), light.NewActualProvider(c), light.NewHueApplier(c))
	o := reconcile.NewOrchestrator(cfg.Reconciler.GetPeriodicInterval(), cfg.Reconciler.GetDebounceMs(), cfg.Reconciler.GetRateLimitRPS())
	if err := o.SetDB(db); err != nil {
		return nil, err
	}
	o.SetReassert(c.ReassertDesired)
	o.Register(gp)
	o.Register(lp)
	es := v2.NewEventStreamWithConfig(c, v2.EventStreamConfig{MinBackoff: cfg.Events.SSE.GetMinRetryBackoff(), MaxBackoff: cfg.Events.SSE.GetMaxRetryBackoff(), Multiplier: cfg.Events.SSE.GetRetryMultiplier(), MaxReconnects: cfg.Events.SSE.GetMaxReconnects()})
	es.OnConnect = c.Refresh
	return &HueService{cfg: cfg, Client: c, SceneIndex: scenes, EventStream: es, Orchestrator: o, Bus: events.NewBusWithConfig(cfg.EventBus.GetWorkers(), cfg.EventBus.GetQueueSize()), Stores: stores, GroupProvider: gp, LightProvider: lp}, nil
}
func (s *HueService) Start(ctx context.Context) error { return s.Client.Connect(ctx) }
func (s *HueService) StartBackground(ctx context.Context, onFatalError func(error)) {
	ctx, s.cancel = context.WithCancel(ctx)
	if s.cfg.Events.SSE.IsEnabled() {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			if err := s.EventStream.Run(ctx, s.Bus); err != nil && onFatalError != nil {
				onFatalError(err)
			}
		}()
	}
	if s.cfg.Reconciler.IsEnabled() {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			if err := s.Orchestrator.Run(ctx); err != nil && onFatalError != nil {
				onFatalError(err)
			}
		}()
	}
}

// Stop cancels ingress/reconciliation and joins before their DB/client is freed.
// The application owns draining and closing the bus after producers have stopped.
func (s *HueService) Stop(ctx context.Context) error {
	if s.cancel != nil {
		s.cancel()
	}
	done := make(chan struct{})
	go func() { s.wg.Wait(); close(done) }()
	select {
	case <-done:
		return s.Client.Close()
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (s *HueService) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.GetShutdownTimeout())
	defer cancel()
	_ = s.Stop(ctx)
	if s.Bus != nil {
		s.Bus.Close(ctx)
	}
}
