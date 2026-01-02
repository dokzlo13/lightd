package lua

import (
	"github.com/amimof/huego"

	"github.com/dokzlo13/lightd/internal/actions"
	"github.com/dokzlo13/lightd/internal/cache"
	"github.com/dokzlo13/lightd/internal/config"
	"github.com/dokzlo13/lightd/internal/geo"
	"github.com/dokzlo13/lightd/internal/kv"
	"github.com/dokzlo13/lightd/internal/reconcile"
	"github.com/dokzlo13/lightd/internal/scheduler"
	"github.com/dokzlo13/lightd/internal/stores"
)

// RuntimeDeps groups all dependencies needed by Lua runtime.
// This reduces constructor parameter count and makes dependencies explicit.
type RuntimeDeps struct {
	Config       *config.Config
	Registry     *actions.Registry
	Invoker      *actions.Invoker
	Scheduler    *scheduler.Scheduler
	Bridge       *huego.Bridge
	SceneIndex   *cache.SceneIndex
	Stores       *stores.Registry
	Orchestrator *reconcile.Orchestrator
	GeoCalc      *geo.Calculator
	KVManager    *kv.Manager
}
