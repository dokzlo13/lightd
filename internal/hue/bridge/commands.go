package bridge

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	openhue "github.com/openhue/openhue-go"
)

type desiredKey struct{}
type DesiredRequest struct {
	Kind, ID string
	Version  int64
}

func WithDesired(ctx context.Context, kind, id string, version int64) context.Context {
	return context.WithValue(ctx, desiredKey{}, DesiredRequest{kind, id, version})
}
func desiredRequest(ctx context.Context) (DesiredRequest, bool) {
	if ctx == nil {
		return DesiredRequest{}, false
	}
	v, ok := ctx.Value(desiredKey{}).(DesiredRequest)
	return v, ok
}

// fence persists supersession before any direct request is attempted. Even a
// failed newer direct command must not later be undone by an older retry.
func (c *Client) fence(ctx context.Context, ids []string) error {
	if c.db == nil {
		return nil
	}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT kind,id,version FROM resource_state WHERE kind IN ('group','light')`)
	if err != nil {
		return err
	}
	var versions []DesiredRequest
	for rows.Next() {
		var d DesiredRequest
		if err = rows.Scan(&d.Kind, &d.ID, &d.Version); err != nil {
			rows.Close()
			return err
		}
		versions = append(versions, d)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, id := range ids {
		for _, d := range versions {
			if _, err = tx.ExecContext(ctx, `INSERT INTO hue_manual_fences_v1(light_id,kind,resource_id,version) VALUES(?,?,?,?) ON CONFLICT(light_id,kind,resource_id) DO UPDATE SET version=MAX(version,excluded.version)`, id, d.Kind, d.ID, d.Version); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
func (c *Client) allowed(ctx context.Context, id string) (bool, error) {
	d, ok := desiredRequest(ctx)
	if !ok || c.db == nil {
		return true, nil
	}
	var version int64
	err := c.db.QueryRowContext(ctx, `SELECT version FROM resource_state WHERE kind=? AND id=?`, d.Kind, d.ID).Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if version != d.Version {
		return false, nil
	}
	var fence int64
	err = c.db.QueryRowContext(ctx, `SELECT version FROM hue_manual_fences_v1 WHERE light_id=? AND kind=? AND resource_id=?`, id, d.Kind, d.ID).Scan(&fence)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return d.Version > fence, nil
}
func (c *Client) ApplyLight(ctx context.Context, id string, s State) error {
	if err := ValidateID(id); err != nil {
		return err
	}
	return c.apply(ctx, map[string]State{id: s})
}
func (c *Client) ApplyGroup(ctx context.Context, id string, s State, sceneName string) error {
	if err := s.Validate(); err != nil {
		return err
	}
	g, err := c.group(id)
	if err != nil {
		return err
	}
	plans := map[string]State{}
	if sceneName != "" {
		scene, err := c.ResolveScene(id, sceneName)
		if err != nil {
			return err
		}
		members := map[string]bool{}
		for _, id := range g.Members {
			members[id] = true
		}
		for id, p := range scene.Members {
			if !members[id] {
				continue
			}
			p.On = s.On
			p.PowerMode = s.PowerMode
			p.Duration = s.Duration
			if s.Brightness != nil {
				p.Brightness = s.Brightness
			}
			plans[id] = p
		}
		if len(plans) == 0 {
			return fmt.Errorf("scene has no applicable light actions")
		}
	} else {
		for _, id := range g.Members {
			plans[id] = s
		}
	}
	return c.apply(ctx, plans)
}
func (c *Client) apply(ctx context.Context, plans map[string]State) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ids := make([]string, 0, len(plans))
	for id, s := range plans {
		if err := ValidateID(id); err != nil {
			return err
		}
		if err := s.Validate(); err != nil {
			return err
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	c.commandMu.Lock()
	defer c.commandMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, ok := desiredRequest(ctx); !ok {
		if err := c.fence(ctx, ids); err != nil {
			return fmt.Errorf("persist manual authority: %w", err)
		}
	}
	var errs []error
	for _, id := range ids {
		s := plans[id]
		if s.PowerMode == "next_on" {
			continue
		}
		allowed, err := c.allowed(ctx, id)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !allowed {
			continue
		}
		actual, err := c.Light(ctx, id)
		if err != nil {
			errs = append(errs, fmt.Errorf("light %s: %w", id, err))
			continue
		}
		// An appearance-only command never turns a lamp on, including a lamp that
		// was switched off externally between this observation and the write.
		if s.On == nil && (actual.State.On == nil || !*actual.State.On) {
			continue
		}
		if s.Ct != nil {
			if actual.CTMin == 0 || actual.CTMax == 0 {
				errs = append(errs, fmt.Errorf("light %s has no color temperature capability", id))
				continue
			}
			v := int(*s.Ct)
			if v < actual.CTMin {
				v = actual.CTMin
			}
			if v > actual.CTMax {
				v = actual.CTMax
			}
			ct := uint16(v)
			s.Ct = &ct
		}
		if s.Effect != "" && !contains(actual.Effects, s.Effect) {
			errs = append(errs, fmt.Errorf("light %s does not support effect %q", id, s.Effect))
			continue
		}
		if !s.HasAppearance() && s.On == nil {
			continue
		}
		if err := c.lightLimit.Wait(ctx); err != nil {
			return err
		}
		// Recheck after rate limiting: a newer persisted intent may have arrived.
		allowed, err = c.allowed(ctx, id)
		if err != nil {
			return err
		}
		if !allowed {
			continue
		}
		err = c.putLight(ctx, id, s)
		if err != nil {
			errs = append(errs, fmt.Errorf("light %s: %w", id, err))
		}
	}
	err := errors.Join(errs...)
	c.record(err)
	return err
}
func contains(values []string, s string) bool {
	for _, v := range values {
		if v == s {
			return true
		}
	}
	return false
}
func statePut(s State) openhue.LightPut {
	p := openhue.LightPut{}
	if s.On != nil {
		p.On = &openhue.On{On: s.On}
	}
	if s.Brightness != nil {
		b := openhue.Brightness(*s.Brightness)
		p.Dimming = &openhue.Dimming{Brightness: &b}
	}
	if s.Ct != nil {
		v := int(*s.Ct)
		p.ColorTemperature = &openhue.ColorTemperature{Mirek: &v}
	}
	if len(s.Xy) == 2 {
		x, y := s.Xy[0], s.Xy[1]
		p.Color = &openhue.Color{Xy: &openhue.GamutPosition{X: &x, Y: &y}}
	}
	if s.Effect != "" {
		e := openhue.SupportedEffects(s.Effect)
		p.Effects = &openhue.Effects{Effect: &e}
	}
	// The generated version represents light dynamics as an anonymous struct.
	// Decode its typed JSON once so explicit zero duration stays present.
	if s.Duration != nil {
		b, _ := json.Marshal(map[string]any{"dynamics": map[string]any{"duration": *s.Duration}})
		var d openhue.LightPut
		_ = json.Unmarshal(b, &d)
		p.Dynamics = d.Dynamics
	}
	return p
}
func (c *Client) putLight(ctx context.Context, id string, s State) error {
	ctx, cancel := c.context(ctx)
	defer cancel()
	r, err := c.api.UpdateLightWithResponse(ctx, id, statePut(s))
	if err != nil {
		return err
	}
	return check(r.StatusCode(), r.Body)
}

// Snapshot captures each member independently. Restore applies appearance only
// by default and does not resurrect lamps switched off after capture.
func (c *Client) Snapshot(ctx context.Context, kind, id string) (Snapshot, error) {
	ids := []string{id}
	if kind == "group" {
		g, err := c.group(id)
		if err != nil {
			return Snapshot{}, err
		}
		ids = g.Members
	} else if kind != "light" {
		return Snapshot{}, fmt.Errorf("snapshot kind must be group or light")
	}
	out := Snapshot{Lights: map[string]State{}}
	for _, id := range ids {
		l, err := c.Light(ctx, id)
		if err != nil {
			return Snapshot{}, err
		}
		out.Lights[id] = l.State
	}
	return out, nil
}
func (c *Client) Restore(ctx context.Context, s Snapshot, restorePower bool) error {
	plans := map[string]State{}
	for id, p := range s.Lights {
		if !restorePower {
			p = p.Appearance()
		}
		plans[id] = p
	}
	return c.apply(ctx, plans)
}

// Signal invokes supported native signaling. It is a direct event, never
// automatically retried or stored as desired state.
func (c *Client) Signal(ctx context.Context, kind, id, signal string, duration int) error {
	if duration < 0 || duration > 65534000 {
		return fmt.Errorf("signal duration must be 0..65534000 milliseconds")
	}
	ids := []string{id}
	if kind == "group" {
		g, err := c.group(id)
		if err != nil {
			return err
		}
		ids = g.Members
	} else if kind != "light" {
		return fmt.Errorf("signal kind must be group or light")
	}
	c.commandMu.Lock()
	defer c.commandMu.Unlock()
	if err := c.fence(ctx, ids); err != nil {
		return err
	}
	var errs []error
	for _, id := range ids {
		l, err := c.Light(ctx, id)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !contains(l.Signals, signal) {
			errs = append(errs, fmt.Errorf("light %s does not support signal %q", id, signal))
			continue
		}
		if err = c.lightLimit.Wait(ctx); err != nil {
			return err
		}
		var p openhue.LightPut
		b, _ := json.Marshal(map[string]any{"signaling": map[string]any{"signal": signal, "duration": duration}})
		if err = json.Unmarshal(b, &p); err != nil {
			return err
		}
		callCtx, cancel := c.context(ctx)
		r, err := c.api.UpdateLightWithResponse(callCtx, id, p)
		cancel()
		if err == nil {
			err = check(r.StatusCode(), r.Body)
		}
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// WaitUntil is cancellation aware and used by retries without blocking shutdown.
func WaitUntil(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
