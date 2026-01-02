package lua

import (
	"github.com/amimof/huego"

	"github.com/dokzlo13/lightd/internal/actions"
	"github.com/dokzlo13/lightd/internal/config"
	"github.com/dokzlo13/lightd/internal/geo"
	"github.com/dokzlo13/lightd/internal/hue"
	"github.com/dokzlo13/lightd/internal/hue/reconcile"
	"github.com/dokzlo13/lightd/internal/scheduler"
	"github.com/dokzlo13/lightd/internal/storage/kv"
)

// RuntimeDeps groups all dependencies needed by Lua runtime.
// This reduces constructor parameter count and makes dependencies explicit.
type RuntimeDeps struct {
	Config       *config.Config
	Registry     *actions.Registry
	Invoker      *actions.Invoker
	Scheduler    *scheduler.Scheduler
	Bridge       *huego.Bridge
	SceneIndex   *hue.SceneIndex
	Stores       *hue.StoreRegistry
	Orchestrator *reconcile.Orchestrator
	GeoCalc      *geo.Calculator
	KVManager    *kv.Manager
}
