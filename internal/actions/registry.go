package actions

import (
	"fmt"
	"sync"
)

// Action represents a named, invokable unit of work
type Action interface {
	Name() string
	Execute(ctx *Context, args map[string]any) error
}

// SimpleAction is the standard action implementation
type SimpleAction struct {
	name string
	fn   func(ctx *Context, args map[string]any) error
}

func (a *SimpleAction) Name() string { return a.name }

func (a *SimpleAction) Execute(ctx *Context, args map[string]any) error {
	return a.fn(ctx, args)
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

// RegisterSimple adds a simple action (convenience method)
func (r *Registry) RegisterSimple(name string, fn func(ctx *Context, args map[string]any) error) error {
	return r.Register(&SimpleAction{name: name, fn: fn})
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
