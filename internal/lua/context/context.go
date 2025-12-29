// Package context provides modular context building for Lua action functions.
//
// The context table (ctx) is passed to Lua actions and provides access to
// system functionality like actual state, desired state, and reconciliation.
//
// This follows the same pattern as Lua modules (PreloadModule) but for the
// action context table. Each ContextModule knows how to install its functionality.
package context

import (
	"context"

	lua "github.com/yuin/gopher-lua"
)

// ContextModule installs functionality into the action context table.
// This mirrors how Lua modules have a Loader method.
type ContextModule interface {
	// Name returns the field name in ctx (e.g., "actual", "desired").
	// Empty string means install directly into the root context table.
	Name() string

	// Install adds this module's functionality to the context table.
	// L is the Lua state, ctx is the context table being built,
	// goCtx is the Go context for cancellation/timeouts.
	Install(L *lua.LState, ctx *lua.LTable, goCtx context.Context)
}

// Builder collects context modules and builds the final ctx table.
// It's similar to how Runtime registers Lua modules.
type Builder struct {
	modules []ContextModule
}

// NewBuilder creates a new context builder.
func NewBuilder() *Builder {
	return &Builder{
		modules: make([]ContextModule, 0),
	}
}

// Register adds a context module to the builder.
func (b *Builder) Register(m ContextModule) *Builder {
	b.modules = append(b.modules, m)
	return b
}

// Build creates the ctx table for a Lua action invocation.
// Each registered module installs its functionality into the table.
func (b *Builder) Build(L *lua.LState, goCtx context.Context) *lua.LTable {
	ctx := L.NewTable()
	for _, m := range b.modules {
		m.Install(L, ctx, goCtx)
	}
	return ctx
}
