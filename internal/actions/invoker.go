package actions

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dokzlo13/lightd/internal/storage"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

var ErrAdmissionRejected = errors.New("action queue admission rejected")

type Executor interface {
	Do(context.Context, func(context.Context)) bool
}
type Metadata struct {
	PlannedAt     time.Time
	PolicyVersion string
	Generation    int64
	EventIDs      []string
}
type metadataKey struct{}
type invocationKey struct{}
type invocationContext struct {
	ID      string
	Invoker *Invoker
}

func WithMetadata(ctx context.Context, m Metadata) context.Context {
	return context.WithValue(ctx, metadataKey{}, m)
}
func InvocationID(ctx context.Context) string {
	v, _ := ctx.Value(invocationKey{}).(invocationContext)
	return v.ID
}

// Invoker records receipts and terminal outcomes independently of deduplication.
type Invoker struct {
	registry   *Registry
	ledger     *storage.Ledger
	ctxFactory func(context.Context) *Context
}

func NewInvoker(r *Registry, l *storage.Ledger, f func(context.Context) *Context) *Invoker {
	return &Invoker{r, l, f}
}
func (i *Invoker) HasAction(name string) bool { _, ok := i.registry.Get(name); return ok }
func (i *Invoker) receipt(ctx context.Context, name, key, source, def string) (storage.Invocation, bool, error) {
	if source == "" {
		source = "manual"
	}
	m, _ := ctx.Value(metadataKey{}).(Metadata)
	in := storage.Invocation{ID: uuid.NewString(), Action: name, Key: key, Source: source, Definition: def, ParentID: InvocationID(ctx), PlannedAt: m.PlannedAt, PolicyVersion: m.PolicyVersion, Generation: m.Generation, EventIDs: m.EventIDs}
	if in.ParentID != "" {
		in.EventIDs = nil
	}
	ok, err := i.ledger.Receive(ctx, in)
	return in, ok, err
}
func (i *Invoker) Invoke(ctx context.Context, name string, args map[string]any, key string) error {
	return i.InvokeWithSource(ctx, name, args, key, "manual", "")
}
func (i *Invoker) InvokeWithSource(ctx context.Context, name string, args map[string]any, key, source, def string) error {
	in, ok, err := i.receipt(ctx, name, key, source, def)
	if err != nil || !ok {
		return err
	}
	return i.execute(ctx, in, func(context.Context) (map[string]any, error) { return args, nil })
}
func (i *Invoker) Queue(ctx context.Context, executor Executor, name string, args map[string]any, key, source, def string) error {
	return i.QueuePrepared(ctx, executor, name, key, source, def, func(context.Context) (map[string]any, error) { return args, nil })
}

// QueuePrepared persists receipt before admission. prepare runs on the Lua worker,
// so reducers can safely use Lua. A reducer failure never invokes the action.
func (i *Invoker) QueuePrepared(ctx context.Context, executor Executor, name, key, source, def string, prepare func(context.Context) (map[string]any, error)) error {
	in, ok, err := i.receipt(ctx, name, key, source, def)
	if err != nil || !ok {
		return err
	}
	if executor.Do(ctx, func(workCtx context.Context) {
		if err := i.execute(workCtx, in, prepare); err != nil {
			log.Error().Err(err).Str("invocation_id", in.ID).Str("action", name).Msg("Action failed")
		}
	}) {
		return nil
	}
	recordErr := i.finish(in, "rejected", ErrAdmissionRejected, nil)
	return errors.Join(ErrAdmissionRejected, recordErr)
}
func (i *Invoker) finish(in storage.Invocation, status string, err error, actx *Context) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if actx == nil {
		return i.ledger.FinishInvocation(ctx, in, status, err, nil)
	}
	return i.ledger.FinishInvocation(ctx, in, status, err, actx.commit)
}
func (i *Invoker) execute(ctx context.Context, in storage.Invocation, prepare func(context.Context) (map[string]any, error)) (err error) {
	completed := false
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("action panicked: %v", p)
		}
		if err != nil && !completed {
			err = errors.Join(err, i.finish(in, "failed", err, nil))
		}
	}()
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = i.ledger.StartInvocation(ctx, in); err != nil {
		return err
	}
	ctx = context.WithValue(ctx, invocationKey{}, invocationContext{ID: in.ID, Invoker: i})
	args, err := prepare(ctx)
	if err != nil {
		return err
	}
	action, ok := i.registry.Get(in.Action)
	if !ok {
		return fmt.Errorf("action %q not found", in.Action)
	}
	actx := i.ctxFactory(ctx)
	actx.runAction = func(name string, args map[string]any) error {
		return i.InvokeWithSource(ctx, name, args, "", "lua", "")
	}
	log.Info().Str("action", in.Action).Str("source", in.Source).Str("invocation_id", in.ID).Msg("Executing action")
	if err = action.Execute(actx, args); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = i.finish(in, "completed", nil, actx); err != nil {
		return fmt.Errorf("commit action outcome: %w", err)
	}
	completed = true
	actx.committed()
	return nil
}

func (i *Invoker) ReceiveEvent(ctx context.Context, id, source string) (bool, error) {
	return i.ledger.ReceiveEvent(ctx, id, source)
}
func (i *Invoker) EndEvents(ids []string, status string, err error) error {
	return i.ledger.EndEvents(ids, status, err)
}

// WithParent propagates correlation without inheriting the parent's execution deadline.
func WithParent(ctx context.Context, parentID string) context.Context {
	return context.WithValue(ctx, invocationKey{}, invocationContext{ID: parentID})
}
