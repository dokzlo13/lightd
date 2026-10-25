# Native Hue control

Lightd uses OpenHue's generated V2 REST client and go-sse for event framing. This release replaces the V1-shaped Lua controls. Use native resource UUIDs, percentage brightness and millisecond durations. No V1 transport or compatibility translator remains.

## Immediate control

```lua
local hue = require("hue")
local room, err = hue.group("10000000-0000-0000-0000-000000000001")
if err then error(err) end

room:set_brightness(45)               -- percent; keeps off members off
room:set_ct(350)                      -- mirek, limited to each lamp's capability
room:set_color(0.40, 0.35)            -- CIE xy
room:set_hsv(30, 80)                  -- degrees and saturation percent
room:set_state({brightness=30, ct=400, duration=1500})
room:set_scene("Relax")              -- currently-on members, appearance only
room:set_scene("Relax", true, 1000)  -- explicitly power on scene members
room:off()
```

The example UUID is synthetic. Configure real IDs privately. A group ID may identify a room, zone or grouped-light resource. Scene UUIDs avoid ambiguity; names must be unique within the chosen group.

`hue.group(id)` and `hue.light(id)` return `(object, nil)` or `(nil, error)`. Setters return the object for chaining and **raise a Lua error on failure**. Getters and listing/refresh operations also raise on failure. Use `pcall` when handling a failure explicitly. Logging an error and returning normally from an action still means the action returned successfully.

`hue.groups()` and `hue.lights()` return arrays of resource objects. `hue.scenes()` returns scene records with `id`, `name`, `group_id` and member appearance. `hue.refresh()` atomically refreshes group membership, scenes and connectivity metadata. Startup and event-stream reconnection refresh the catalog automatically. Refresh explicitly after editing resources elsewhere, then publish a new desired frame to adopt changed membership.

| Object method | Meaning |
|---|---|
| `id()`, `name()` | Native identity and display name |
| `is_on()`, `any_on()`, `all_on()` | Fresh power observation; group `all_on` checks individual members |
| `members()` | Group member light UUIDs |
| `get_brightness()` | Light percentage or mean percentage of currently-on group members; nil when unavailable |
| `get_state()` | Native light/group record; a group summary has no representative member color |
| `on()`, `off()`, `toggle()` | Explicit power; group toggle uses fresh `any_on` |
| `set_brightness(percent)` | Appearance-only brightness, 0..100; zero means minimum brightness, not off |
| `set_ct(mirek)` | Appearance-only temperature |
| `set_color(x,y)`, `set_hsv(degrees,percent)` | Appearance-only color; HSV conversion does not set brightness |
| `set_scene(scene[, turn_on=false[, duration_ms]])` | Group scene with optional explicit power-on and transition |
| `set_state(table)` | Sparse native update |
| `snapshot()`, `restore(snapshot[, restore_power=false])` | Per-member capture and restoration |
| `effect(name)` | Native effect from the light's `get_state().effects` capability list |
| `signal(name,duration_ms)` | Native attention signal from `get_state().signals` |

`set_state` accepts `on` (boolean), `brightness` (0..100), `ct` (positive mirek), `xy` (two coordinates), `duration` (0..3,600,000 ms), `effect` and native `gradient` data. Optional `power_mode="preserve"` requires omitted `on`; `"enforce"` requires explicit `on`. Omitted values, false and zero remain distinct on the wire. CT and XY cannot be combined. Unknown and legacy fields fail explicitly. Persistent `next_on` mode belongs to desired state, not immediate calls.

Signals are intentional attention effects and can temporarily change visible power. They are direct calls and are never automatically retried. An `on_off` signal can replace an old attention blink when supported. V1 `colorloop`, independent hue/saturation setters and V1 alert strings have no identical native replacement. Choose a supported effect/signal or paired HSV/XY; unsupported values produce errors.

## Snapshot and restore

```lua
local saved = room:snapshot()
room:set_color(0.30, 0.60)
-- Schedule restoration in another action when an extended pause is needed.
room:restore(saved)
```

Snapshots preserve each member's own appearance, including native gradients. Default restoration affects members currently on and omits power commands. Passing true explicitly restores captured power. A group's mean brightness is useful for a dial baseline, but never proves that every member matches a requested brightness.

## Desired state and ownership

Desired builders use the same native IDs and appearance units. `set_brightness`, `set_ct`, `set_color`, paired `set_hsv`, `set_transition(ms)` and scene selection prepare persistent intent. `preserve_power()` clears saved power intent; `on()`/`off()` enforce power; `next_on()` stores appearance without immediately changing lamps. Automatic phases should explicitly preserve power.

Desired state, per-member authority and the action outcome commit together. New group/light intent supersedes older intent for overlapping lamps. Direct commands claim durable member authority before sending. Old retries cannot overwrite a newer direct adjustment after restart. A fresh desired publication or explicit forced reconciliation can take authority again. Force is an explicit reassertion, not a read operation.

Each completed member is recorded separately. Retries skip acknowledged and superseded members. Preparation and rate waits occur outside a cancellation-aware command gate; final checking, sending and acknowledgment share that gate with desired publication. An already-started HTTP request completes or reaches its configured timeout before a later publication. Hue offers no distributed transaction: if a response is lost after acceptance, an absolute-state retry can repeat the request.

Retries start at one second, cap at 30 seconds and expire after 10 minutes of active age. Each attempt has a 30-second total context in addition to request timeouts. Retry age and delivery status survive restart. Expired work remains visible and needs a newer desired version or explicit force. Periodic configuration does not silently enable continual physical drift enforcement.

The delivery journal distinguishes pending, acknowledged, superseded, expired and imported versions. A bridge acknowledgment is not proof of physical convergence. During first migration without delivery/authority journals, historical desired rows become imported baselines and do not control lights before the new startup policy runs. Fresh boot-policy intent establishes the current phase.

## Migration and operations

Back up SQLite before updating the image or script. Update scripts and desired payloads together:

- Replace numeric light/group IDs with native UUIDs; prefer scene UUIDs when names may collide.
- Replace `set_bri(n)`/`get_bri()` with percentage `set_brightness(n*100/254)`/`get_brightness()`.
- Replace `transitiontime` deciseconds with `duration` milliseconds (`old*100`), or desired `set_transition(ms)`.
- Replace a saved group action color with per-member `snapshot()`/`restore()`.
- Replace independent hue/saturation setters with paired HSV or XY; select supported effects/signals explicitly.
- Handle immediate failures as Lua errors. Avoid treating a logged failure as successful execution.
- Use `preserve_power()` for automatic phases to clear historical sticky on/off intent.
- Clear `hue_target_authority_v1` and `hue_delivery_v1` alongside explicit desired-state migration/reset. Preserve them on ordinary restarts.

The application stops and joins Hue stream/reconciliation workers before database teardown. Client, event-stream and orchestrator status snapshots expose dependency/retry/delivery information. The bridge validates Hue certificate chains without changing global HTTP defaults. Hostname matching is skipped because a configured local IP is not the certificate identity. Native authentication uses headers, not credential-bearing V1 URLs.

The embedded Hue roots are derived from [OpenHue Go](https://github.com/openhue/openhue-go/blob/876da6d4b966086cb539c92dcd6f626473d10287/certs.go), licensed under [Apache-2.0](https://github.com/openhue/openhue-go/blob/876da6d4b966086cb539c92dcd6f626473d10287/LICENSE).
