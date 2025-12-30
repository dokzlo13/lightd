// Package context provides modular context building for Lua action functions.
//
// The context table (ctx) is passed to Lua actions and provides access to
// system functionality like actual state, desired state, and reconciliation.
//
// This follows the same pattern as Lua modules (PreloadModule) but for the
// action context table. Each ContextModule knows how to install its functionality.
package context

import (
	lua "github.com/yuin/gopher-lua"
)

// ContextModule installs functionality into the action context table.
// This mirrors how Lua modules have a Loader method.
type ContextModule interface {
	// Name returns the field name in ctx (e.g., "actual", "desired").
	// Empty string means install directly into the root context table.
	Name() string

	// Install adds this module's functionality to the context table.
	// L is the Lua state, ctx is the context table being built.
	// Modules should use L.Context() to get the Go context for cancellation.
	Install(L *lua.LState, ctx *lua.LTable)
}

// CleanupModule is an optional interface for modules that need cleanup after action execution.
// If a ContextModule implements this, Cleanup() is called after the action completes.
// This ensures pending state is always persisted.
type CleanupModule interface {
	// Cleanup is called after a Lua action completes (success or error).
	// Use this to flush pending state that wasn't explicitly committed.
	Cleanup()
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
// Modules use L.Context() to access the Go context for cancellation.
func (b *Builder) Build(L *lua.LState) *lua.LTable {
	ctx := L.NewTable()
	for _, m := range b.modules {
		m.Install(L, ctx)
	}
	return ctx
}

// Cleanup calls Cleanup() on all modules that implement CleanupModule.
// This should be called after every action execution to ensure pending state is flushed.
func (b *Builder) Cleanup() {
	for _, m := range b.modules {
		if cm, ok := m.(CleanupModule); ok {
			cm.Cleanup()
		}
	}
}
