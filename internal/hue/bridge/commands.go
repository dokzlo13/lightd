package bridge

import (
	"bytes"
	"context"
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
	var tokens map[string]targetAuthority
	if _, desired := desiredRequest(ctx); !desired {
		var err error
		tokens, err = c.admitDirect(ctx, ids)
		if err != nil {
			return fmt.Errorf("persist manual authority: %w", err)
		}
	}
	var errs []error
	superseded := 0
	for _, id := range ids {
		s := plans[id]
		if s.PowerMode == "next_on" {
			continue
		}
		// Preparation and pacing do not hold the final-send/publication gate.
		actual, err := c.Light(ctx, id)
		if err != nil {
			errs = append(errs, fmt.Errorf("light %s: %w", id, err))
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
		off := s.On == nil && (actual.State.On == nil || !*actual.State.On)
		if err = c.lightLimit.Wait(ctx); err != nil {
			return err
		}
		err = c.targetSend(ctx, id, tokens, func() error {
			if off || (!s.HasAppearance() && s.On == nil) {
				return nil
			}
			return c.putLight(ctx, id, s)
		})
		if errors.Is(err, ErrSuperseded) {
			superseded++
			continue
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("light %s: %w", id, err))
		}
	}
	err := errors.Join(errs...)
	c.record(err)
	if err == nil && len(ids) > 0 && superseded == len(ids) {
		return ErrSuperseded
	}
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
	body, err := json.Marshal(statePut(s))
	if err != nil {
		return err
	}
	if len(s.Gradient) > 0 {
		var fields map[string]json.RawMessage
		if err = json.Unmarshal(body, &fields); err != nil {
			return err
		}
		fields["gradient"] = s.Gradient
		body, err = json.Marshal(fields)
		if err != nil {
			return err
		}
	}
	r, err := c.api.UpdateLightWithBodyWithResponse(ctx, id, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	return checkWrite(r.StatusCode(), r.Body, id)
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
	tokens, err := c.admitDirect(ctx, ids)
	if err != nil {
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
		err = c.targetSend(ctx, id, tokens, func() error {
			callCtx, cancel := c.context(ctx)
			defer cancel()
			r, err := c.api.UpdateLightWithResponse(callCtx, id, p)
			if err != nil {
				return err
			}
			return checkWrite(r.StatusCode(), r.Body, id)
		})
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
