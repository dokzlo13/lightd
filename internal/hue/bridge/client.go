package bridge

import (
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	openhue "github.com/openhue/openhue-go"
	"golang.org/x/time/rate"
)

type Status struct {
	Connected   bool      `json:"connected"`
	LastSuccess time.Time `json:"last_success,omitempty"`
	LastError   string    `json:"last_error,omitempty"`
}

// Client owns policy around the generated SDK: deadlines, sparse commands,
// resource catalog, per-light serialization and persisted manual authority.
type Client struct {
	api                    *openhue.ClientWithResponses
	http                   *http.Client
	base, address, token   string
	db                     *sql.DB
	timeout                time.Duration
	mu                     sync.RWMutex
	groups                 map[string]Group
	scenes                 map[string]Scene
	status                 Status
	commandMu              sync.Mutex
	lightLimit, groupLimit *rate.Limiter
}

func New(address, token string, timeout time.Duration) *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = (&net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}).DialContext
	transport.TLSHandshakeTimeout = timeout
	transport.ResponseHeaderTimeout = timeout
	// Hue uses locally issued certificates. TLS verification is scoped to this
	// bridge client and never changes the process-wide HTTP defaults.
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: true}
	c, _ := NewWithHTTP(address, token, &http.Client{Transport: transport, Timeout: timeout}, timeout)
	return c
}
func NewWithHTTP(address, token string, h *http.Client, timeout time.Duration) (*Client, error) {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	base := strings.TrimRight(address, "/")
	if !strings.Contains(base, "://") {
		base = "https://" + base
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("invalid Hue bridge address")
	}
	if h == nil {
		return nil, fmt.Errorf("HTTP client is required")
	}
	c := &Client{http: h, base: base, address: u.Host, token: token, timeout: timeout, groups: map[string]Group{}, scenes: map[string]Scene{}, lightLimit: rate.NewLimiter(10, 1), groupLimit: rate.NewLimiter(1, 1)}
	c.api, err = openhue.NewClientWithResponses(base, openhue.WithHTTPClient(h), openhue.WithRequestEditorFn(func(ctx context.Context, r *http.Request) error {
		r.Header.Set("hue-application-key", token)
		return nil
	}))
	return c, err
}
func (c *Client) SetDB(db *sql.DB) error {
	c.db = db
	if db == nil {
		return nil
	}
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS hue_manual_fences_v1 (light_id TEXT NOT NULL,kind TEXT NOT NULL,resource_id TEXT NOT NULL,version INTEGER NOT NULL,PRIMARY KEY(light_id,kind,resource_id))`)
	return err
}
func (c *Client) Address() string          { return c.address }
func (c *Client) Token() string            { return c.token }
func (c *Client) BaseURL() string          { return c.base }
func (c *Client) HTTPClient() *http.Client { return c.http }
func (c *Client) Timeout() time.Duration   { return c.timeout }
func (c *Client) V1() *Client              { return c } // internal construction compatibility; all traffic uses V2.
func (c *Client) V2() *Client              { return c }
func (c *Client) Bridge() *Client          { return c }
func (c *Client) Close() error             { c.http.CloseIdleConnections(); return nil }
func (c *Client) Status() Status           { c.mu.RLock(); defer c.mu.RUnlock(); return c.status }
func (c *Client) record(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil {
		c.status.Connected = false
		c.status.LastError = err.Error()
	} else {
		c.status.Connected = true
		c.status.LastSuccess = time.Now().UTC()
		c.status.LastError = ""
	}
}
func (c *Client) context(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(ctx, c.timeout)
}

// APIError represents an HTTP or Hue-level rejection. An acknowledged request
// is distinct from an observed physical outcome.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("Hue request failed (%d): %s", e.Status, e.Message)
}
func Permanent(err error) bool {
	var e *APIError
	return errors.As(err, &e) && (e.Status == 400 || e.Status == 401 || e.Status == 403 || e.Status == 404)
}
func check(status int, body []byte) error {
	if len(body) > 8<<20 {
		return fmt.Errorf("Hue response exceeds 8 MiB")
	}
	var env struct {
		Errors []struct {
			Description string `json:"description"`
		} `json:"errors"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("invalid Hue response: %w", err)
	}
	if status < 200 || status >= 300 || len(env.Errors) > 0 {
		parts := []string{}
		for _, e := range env.Errors {
			parts = append(parts, e.Description)
		}
		msg := strings.Join(parts, "; ")
		if msg == "" {
			msg = http.StatusText(status)
		}
		return &APIError{status, msg}
	}
	if len(env.Data) == 0 || string(env.Data) == "null" {
		return fmt.Errorf("Hue response missing data")
	}
	return nil
}
func decodeData[T any](status int, body []byte) ([]T, error) {
	if err := check(status, body); err != nil {
		return nil, err
	}
	var env struct {
		Data []T `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, err
	}
	return env.Data, nil
}
func (c *Client) Lights(ctx context.Context) ([]Light, error) {
	ctx, cancel := c.context(ctx)
	defer cancel()
	r, err := c.api.GetLightsWithResponse(ctx)
	if err != nil {
		c.record(err)
		return nil, err
	}
	items, err := decodeData[openhue.LightGet](r.StatusCode(), r.Body)
	c.record(err)
	if err != nil {
		return nil, err
	}
	out := make([]Light, 0, len(items))
	for _, l := range items {
		if l.Id == nil {
			return nil, fmt.Errorf("light missing ID")
		}
		out = append(out, fromLight(l))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
func (c *Client) Light(ctx context.Context, id string) (Light, error) {
	if err := ValidateID(id); err != nil {
		return Light{}, err
	}
	ctx, cancel := c.context(ctx)
	defer cancel()
	r, err := c.api.GetLightWithResponse(ctx, id)
	if err != nil {
		c.record(err)
		return Light{}, err
	}
	items, err := decodeData[openhue.LightGet](r.StatusCode(), r.Body)
	c.record(err)
	if err != nil {
		return Light{}, err
	}
	if len(items) != 1 || items[0].Id == nil {
		return Light{}, fmt.Errorf("light %s: expected one resource", id)
	}
	return fromLight(items[0]), nil
}
func fromLight(l openhue.LightGet) Light {
	out := Light{ID: *l.Id, Reachable: true}
	if l.Metadata != nil && l.Metadata.Name != nil {
		out.Name = *l.Metadata.Name
	}
	if l.On != nil {
		out.State.On = l.On.On
	}
	if l.Dimming != nil && l.Dimming.Brightness != nil {
		v := float64(*l.Dimming.Brightness)
		out.State.Brightness = &v
	}
	if l.ColorTemperature != nil {
		ct := l.ColorTemperature
		if ct.MirekSchema != nil {
			if ct.MirekSchema.MirekMinimum != nil {
				out.CTMin = *ct.MirekSchema.MirekMinimum
			}
			if ct.MirekSchema.MirekMaximum != nil {
				out.CTMax = *ct.MirekSchema.MirekMaximum
			}
		}
		if ct.Mirek != nil && ct.MirekValid != nil && *ct.MirekValid {
			v := uint16(*ct.Mirek)
			out.State.Ct = &v
			out.State.ColorMode = "ct"
		}
	}
	if out.State.Ct == nil && l.Color != nil && l.Color.Xy != nil && l.Color.Xy.X != nil && l.Color.Xy.Y != nil {
		out.State.Xy = []float32{*l.Color.Xy.X, *l.Color.Xy.Y}
		out.State.ColorMode = "xy"
	}
	if l.Effects != nil {
		if l.Effects.EffectValues != nil {
			for _, e := range *l.Effects.EffectValues {
				out.Effects = append(out.Effects, string(e))
			}
		}
		if l.Effects.Status != nil {
			out.State.Effect = string(*l.Effects.Status)
		}
	}
	if l.Signaling != nil && l.Signaling.SignalValues != nil {
		for _, s := range *l.Signaling.SignalValues {
			out.Signals = append(out.Signals, string(s))
		}
	}
	return out
}
func (c *Client) Connect(ctx context.Context) error { return c.Refresh(ctx) }
func (c *Client) Refresh(ctx context.Context) error {
	ctx, cancel := c.context(ctx)
	defer cancel()
	roomsR, err := c.api.GetRoomsWithResponse(ctx)
	if err != nil {
		c.record(err)
		return err
	}
	rooms, err := decodeData[openhue.RoomGet](roomsR.StatusCode(), roomsR.Body)
	if err != nil {
		c.record(err)
		return err
	}
	zonesR, err := c.api.GetZonesWithResponse(ctx)
	if err != nil {
		c.record(err)
		return err
	}
	zones, err := decodeData[openhue.RoomGet](zonesR.StatusCode(), zonesR.Body)
	if err != nil {
		c.record(err)
		return err
	}
	devicesR, err := c.api.GetDevicesWithResponse(ctx)
	if err != nil {
		c.record(err)
		return err
	}
	devices, err := decodeData[openhue.DeviceGet](devicesR.StatusCode(), devicesR.Body)
	if err != nil {
		c.record(err)
		return err
	}
	scenesR, err := c.api.GetScenesWithResponse(ctx)
	if err != nil {
		c.record(err)
		return err
	}
	scenes, err := decodeData[openhue.SceneGet](scenesR.StatusCode(), scenesR.Body)
	if err != nil {
		c.record(err)
		return err
	}
	byDevice := map[string][]string{}
	for _, d := range devices {
		if d.Id == nil || d.Services == nil {
			continue
		}
		for _, s := range *d.Services {
			if s.Rid != nil && s.Rtype != nil && string(*s.Rtype) == "light" {
				byDevice[*d.Id] = append(byDevice[*d.Id], *s.Rid)
			}
		}
	}
	groups := map[string]Group{}
	for _, r := range append(rooms, zones...) {
		if r.Id == nil {
			continue
		}
		g := Group{ID: *r.Id}
		if r.Metadata != nil && r.Metadata.Name != nil {
			g.Name = *r.Metadata.Name
		}
		if r.Services != nil {
			for _, s := range *r.Services {
				if s.Rid != nil && s.Rtype != nil && string(*s.Rtype) == "grouped_light" {
					g.GroupedID = *s.Rid
				}
			}
		}
		set := map[string]bool{}
		if r.Children != nil {
			for _, ch := range *r.Children {
				if ch.Rid == nil || ch.Rtype == nil {
					continue
				}
				if string(*ch.Rtype) == "light" {
					set[*ch.Rid] = true
				} else if string(*ch.Rtype) == "device" {
					for _, id := range byDevice[*ch.Rid] {
						set[id] = true
					}
				}
			}
		}
		for id := range set {
			g.Members = append(g.Members, id)
		}
		sort.Strings(g.Members)
		groups[g.ID] = g
		if g.GroupedID != "" {
			groups[g.GroupedID] = g
		}
	}
	sc := map[string]Scene{}
	for _, s := range scenes {
		if s.Id == nil || s.Group == nil || s.Group.Rid == nil {
			continue
		}
		v := Scene{ID: *s.Id, GroupID: *s.Group.Rid, Members: map[string]State{}}
		if s.Metadata != nil && s.Metadata.Name != nil {
			v.Name = *s.Metadata.Name
		}
		if s.Actions != nil {
			for _, a := range *s.Actions {
				if a.Target == nil || a.Target.Rid == nil || a.Action == nil {
					continue
				}
				p := State{}
				if a.Action.On != nil {
					p.On = a.Action.On.On
				}
				if a.Action.Dimming != nil && a.Action.Dimming.Brightness != nil {
					b := float64(*a.Action.Dimming.Brightness)
					p.Brightness = &b
				}
				if a.Action.ColorTemperature != nil && a.Action.ColorTemperature.Mirek != nil {
					ct := uint16(*a.Action.ColorTemperature.Mirek)
					p.Ct = &ct
				}
				if a.Action.Color != nil && a.Action.Color.Xy != nil && a.Action.Color.Xy.X != nil && a.Action.Color.Xy.Y != nil {
					p.Xy = []float32{*a.Action.Color.Xy.X, *a.Action.Color.Xy.Y}
					p.Ct = nil
				}
				if a.Action.Effects != nil && a.Action.Effects.Effect != nil {
					p.Effect = string(*a.Action.Effects.Effect)
				}
				v.Members[*a.Target.Rid] = p
			}
		}
		sc[v.ID] = v
	}
	c.mu.Lock()
	c.groups = groups
	c.scenes = sc
	c.mu.Unlock()
	c.record(nil)
	return nil
}
func (c *Client) Group(ctx context.Context, id string) (Group, error) {
	g, err := c.group(id)
	if err != nil {
		return Group{}, err
	}
	lights, err := c.Lights(ctx)
	if err != nil {
		return Group{}, err
	}
	byID := map[string]Light{}
	for _, l := range lights {
		byID[l.ID] = l
	}
	g.AllOn = len(g.Members) > 0
	var sum float64
	count := 0
	for _, id := range g.Members {
		l, ok := byID[id]
		if !ok {
			g.AllOn = false
			continue
		}
		on := l.State.On != nil && *l.State.On
		g.AnyOn = g.AnyOn || on
		g.AllOn = g.AllOn && on
		if on && l.State.Brightness != nil {
			sum += *l.State.Brightness
			count++
		}
	}
	if count > 0 {
		v := sum / float64(count)
		g.State.Brightness = &v
	}
	return g, nil
}
func (c *Client) group(id string) (Group, error) {
	if err := ValidateID(id); err != nil {
		return Group{}, err
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	g, ok := c.groups[id]
	if !ok {
		return Group{}, &APIError{404, "group not in current catalog"}
	}
	g.Members = append([]string(nil), g.Members...)
	return g, nil
}
func (c *Client) Groups() []Group {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := []Group{}
	for id, g := range c.groups {
		if id == g.ID {
			g.Members = append([]string(nil), g.Members...)
			out = append(out, g)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
func (c *Client) Scenes() []Scene {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := []Scene{}
	for _, s := range c.scenes {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
func (c *Client) ResolveScene(groupID, scene string) (Scene, error) {
	g, err := c.group(groupID)
	if err != nil {
		return Scene{}, err
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	var matches []Scene
	for _, s := range c.scenes {
		if s.GroupID == g.ID && (s.ID == scene || s.Name == scene) {
			matches = append(matches, s)
		}
	}
	if len(matches) != 1 {
		return Scene{}, &APIError{404, fmt.Sprintf("scene %q in group: expected one match, found %d", scene, len(matches))}
	}
	return matches[0], nil
}

// readBounded is shared by the SSE error path, never exposing credentials.
func ReadBounded(r io.Reader) string { b, _ := io.ReadAll(io.LimitReader(r, 4096)); return string(b) }
