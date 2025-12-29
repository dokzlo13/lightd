package actions

import (
	"fmt"
	"sync"
)

// Action represents a named, invokable unit of work
type Action interface {
	Name() string
	IsStateful() bool
	// CaptureDecision captures the decision for stateful actions
	// For simple actions, this just returns the args unchanged
	CaptureDecision(ctx *Context, args map[string]any) (map[string]any, error)
	// Execute runs the action with the captured decision
	Execute(ctx *Context, args map[string]any, captured map[string]any) error
}

// SimpleAction is a non-stateful action that doesn't need decision capture
type SimpleAction struct {
	name string
	fn   func(ctx *Context, args map[string]any) error
}

func (a *SimpleAction) Name() string     { return a.name }
func (a *SimpleAction) IsStateful() bool { return false }

func (a *SimpleAction) CaptureDecision(ctx *Context, args map[string]any) (map[string]any, error) {
	// Simple actions don't capture - return args as-is
	return args, nil
}

func (a *SimpleAction) Execute(ctx *Context, args map[string]any, captured map[string]any) error {
	// Simple actions ignore captured and use original args
	return a.fn(ctx, args)
}

// StatefulAction captures a decision before executing
type StatefulAction struct {
	name    string
	capture func(ctx *Context, args map[string]any) (map[string]any, error)
	execute func(ctx *Context, args map[string]any, captured map[string]any) error
}

func (a *StatefulAction) Name() string     { return a.name }
func (a *StatefulAction) IsStateful() bool { return true }

func (a *StatefulAction) CaptureDecision(ctx *Context, args map[string]any) (map[string]any, error) {
	return a.capture(ctx, args)
}

func (a *StatefulAction) Execute(ctx *Context, args map[string]any, captured map[string]any) error {
	return a.execute(ctx, args, captured)
}

// Registry holds all registered actions
type Registry struct {
	mu      sync.RWMutex
	actions map[string]Action
}

// NewRegistry creates a new action registry
func NewRegistry() *Registry {
	return &Registry{
		actions: make(map[string]Action),
	}
}

// Register adds an action to the registry
func (r *Registry) Register(action Action) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.actions[action.Name()]; exists {
		return fmt.Errorf("action %q already registered", action.Name())
	}

	r.actions[action.Name()] = action
	return nil
}

// RegisterSimple adds a simple (non-stateful) action
func (r *Registry) RegisterSimple(name string, fn func(ctx *Context, args map[string]any) error) error {
	return r.Register(&SimpleAction{name: name, fn: fn})
}

// RegisterStateful adds a stateful action with capture and execute phases
func (r *Registry) RegisterStateful(
	name string,
	capture func(ctx *Context, args map[string]any) (map[string]any, error),
	execute func(ctx *Context, args map[string]any, captured map[string]any) error,
) error {
	return r.Register(&StatefulAction{
		name:    name,
		capture: capture,
		execute: execute,
	})
}

// Get retrieves an action by name
func (r *Registry) Get(name string) (Action, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	action, exists := r.actions[name]
	return action, exists
}

// Names returns all registered action names
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.actions))
	for name := range r.actions {
		names = append(names, name)
	}
	return names
}

