# Lightd Developer Manual

This document is the complete reference for the Lua scripting API in Lightd.

---

## Table of Contents

1. [Core Concepts](#core-concepts)
   - [Actions](#actions)
   - [Action Context](#action-context)
2. [Hue API](#hue-api)
   - [Immediate Mode](#immediate-mode)
   - [Reconciled Mode](#reconciled-mode)
   - [How Reconciliation Works](#how-reconciliation-works)
3. [Event Sources](#event-sources)
   - [SSE Events](#sse-events)
   - [Scheduler](#scheduler)
   - [Webhooks](#webhooks)
   - [Event Collection (Debouncing)](#event-collection-debouncing)
4. [KV Storage](#kv-storage)
5. [Utilities](#utilities)
   - [Logging](#logging)
   - [Utils](#utils)
   - [Geo](#geo)
6. [API Reference](#api-reference)

---

## Core Concepts

### Actions

Actions are the fundamental unit of logic in Lightd. An action is a named Lua function that runs in response to events (button presses, schedules, webhooks).

```lua
local action = require("action")

action.define("my_action", function(ctx, args)
    -- ctx: action context (actual state, desired state, reconciler, request)
    -- args: table of arguments passed when the action was triggered
    
    log.info("Action triggered with args: " .. args.foo)
end)
```

**Key points:**
- Actions are registered by name and can be triggered by multiple event sources
- The same action can be called from buttons, schedules, webhooks, or programmatically
- All Lua runs on a single-threaded worker - no race conditions

### Running Actions Programmatically

```lua
-- Run an action immediately (bypasses ledger/deduplication)
action.run("my_action", { foo = "bar" })
```

### Action Context

Every action receives a `ctx` table with access to system functionality:

| Field | Type | Description |
|-------|------|-------------|
| `ctx.actual` | table | Read current Hue state from the bridge |
| `ctx.desired` | table | Declare desired state for reconciliation |
| `ctx:reconcile()` | function | Trigger reconciliation of dirty resources |
| `ctx:force_reconcile()` | function | Force reconciliation of ALL resources |
| `ctx.request` | table/nil | HTTP request data (webhooks only) |

#### ctx.actual

Query the current state of groups:

```lua
local state, err = ctx.actual:group("1")
if err then
    log.error("Failed to get group: " .. err)
    return
end

if state.any_on then
    log.info("At least one light is on")
end
if state.all_on then
    log.info("All lights are on")
end
```

#### ctx.desired

Declare the desired state for groups and lights (see [Reconciled Mode](#reconciled-mode)).

#### ctx.request

For webhook-triggered actions, contains HTTP request data:

```lua
action.define("webhook_handler", function(ctx, args)
    if ctx.request then
        local method = ctx.request.method      -- "POST", "GET", etc.
        local path = ctx.request.path          -- "/lights/toggle"
        local body = ctx.request.body          -- raw body string
        local json = ctx.request.json          -- parsed JSON (table or nil)
        local headers = ctx.request.headers    -- table of headers
        local params = ctx.request.path_params -- e.g., { id = "123" }
    end
end)
```

---

## Hue API

Lightd provides two ways to control lights:

1. **Immediate mode**: Direct API calls with instant feedback
2. **Reconciled mode**: Declare desired state, let the system apply it

### Immediate Mode

Direct control via the `hue` module. Changes happen immediately, no persistence.

```lua
local hue = require("hue")

-- Get a group (returns userdata, error)
local group, err = hue.group("1")
if err then
    log.error("Failed: " .. err)
    return
end

-- Chainable methods
group:on()                         -- turn on
group:off()                        -- turn off
group:toggle()                     -- toggle power
group:set_bri(200)                 -- brightness (1-254)
group:set_scene("Relax")           -- activate scene by name
group:set_color(0.45, 0.41)        -- CIE xy color
group:set_ct(350)                  -- color temp in mirek (153-500)
group:alert("select")              -- flash once

-- Chaining example
group:on():set_scene("Energize"):set_bri(254)

-- Getters
local id = group:id()              -- group ID (number)
local name = group:name()          -- group name
local on = group:any_on()          -- any light on? (bool)
local all = group:all_on()         -- all lights on? (bool)
local bri = group:get_bri()        -- current brightness
local state = group:get_state()    -- { bri, xy, ct, colormode }
local lights = group:lights()      -- table of light IDs

-- Generic state setter (all at once)
group:set_state({
    on = true,
    bri = 200,
    xy = {0.45, 0.41},
    transitiontime = 10,  -- in deciseconds (1 = 100ms)
})
```

#### Light Control

Individual lights work the same way:

```lua
local light, err = hue.light("5")
light:on():set_bri(254):set_color(0.5, 0.4)
```

#### When to Use Immediate Mode

- **Rotary dials**: Real-time brightness adjustment needs instant feedback
- **Visual effects**: Color cycling, alerts, animations
- **Quick toggles**: When you don't need persistence

### Reconciled Mode

Declare what you *want* the lights to be. The reconciler figures out how to get there.

```lua
action.define("set_evening_scene", function(ctx, args)
    -- Declare desired state
    ctx.desired:group("1"):on():set_scene("Relax")
    ctx.desired:group("2"):on():set_scene("Relax")
    
    -- Trigger reconciliation
    ctx:reconcile()
end)
```

#### Desired State Builder

The builder pattern accumulates changes:

```lua
-- Groups
ctx.desired:group("1"):on()                    -- power on
ctx.desired:group("1"):off()                   -- power off
ctx.desired:group("1"):set_scene("Energize")   -- set scene
ctx.desired:group("1"):set_bri(200)            -- brightness
ctx.desired:group("1"):set_color(0.5, 0.4)     -- CIE xy
ctx.desired:group("1"):set_ct(300)             -- color temp
ctx.desired:group("1"):set_hue(40000)          -- hue (0-65535)
ctx.desired:group("1"):set_sat(254)            -- saturation (0-254)

-- Chain everything
ctx.desired:group("1"):on():set_scene("Relax")

-- Lights work the same way
ctx.desired:light("5"):on():set_bri(254)
```

#### When to Use Reconciled Mode

- **Schedules**: Scene changes that should persist across restarts
- **State management**: When you want lightd to track intended state
- **Idempotency**: Multiple triggers won't cause duplicate API calls
- **Recovery**: Desired state is reapplied after bridge reconnects

### How Reconciliation Works

The reconciler implements a "desired state" pattern similar to Kubernetes:

1. **Desired state is persisted**: When you call `ctx.desired:group("1"):set_scene("Relax")`, this is written to SQLite
2. **Actual state is fetched**: The reconciler queries the Hue bridge for current state
3. **Diff is computed**: An FSM determines the minimal action needed:
   - `ActionNone`: Already in desired state, do nothing
   - `ActionTurnOnWithScene`: Group is off, needs to turn on with a scene
   - `ActionApplyScene`: Group is on, needs to change scene
   - `ActionTurnOff`: Group needs to turn off
   - `ActionApplyState`: Apply brightness/color changes
4. **Changes are applied with rate limiting**: The bridge has API limits (~10 req/sec)

```
┌─────────────┐      ┌─────────────┐      ┌─────────────┐
│   Action    │────▶│   Desired   │────▶│ Reconciler  │
│             │      │   Store     │      │             │
└─────────────┘      │  (SQLite)   │      └──────┬──────┘
                     └─────────────┘             │
                                                ▼
                    ┌─────────────┐      ┌─────────────┐
                    │    Hue      │◀────│    FSM      │
                    │   Bridge    │      │   (Diff)    │
                    └─────────────┘      └─────────────┘
```

**Benefits:**
- If lights are already in desired state, no API call is made
- If the bridge is unreachable, desired state persists and will be applied later
- Multiple actions setting the same state are deduplicated
- Rate limiting prevents overwhelming the bridge

#### Reconciler Configuration

```yaml
reconciler:
  enabled: true             # Set false to disable reconciled mode entirely
  periodic_interval: 0      # Periodic reconciliation interval (0 = on-demand only)
  debounce_ms: 0            # Delay before reconciliation (0 = immediate)
  rate_limit_rps: 10.0      # Hue API rate limit (bridge allows ~10 req/sec)
```

When `enabled: false`, `ctx.desired` and `ctx:reconcile()` won't work - use immediate mode only.

---

## Event Sources

### SSE Events

The `events.sse` module handles real-time events from the Hue bridge via SSE (Server-Sent Events).

```lua
local sse = require("events.sse")
```

#### Button Events

```lua
-- Basic button handler
sse.button("resource-id", "short_release", "action_name", { extra = "args" })

-- Button actions:
-- "short_release" - brief press
-- "long_press" - held down
-- "long_release" - released after hold
-- "repeat" - repeated while holding (some buttons)
```

#### Rotary Events

```lua
-- Rotary dial handler
sse.rotary("resource-id", "action_name", { some = "args" })

-- Action receives:
-- args.direction: "clock_wise" or "counter_clock_wise"
-- args.steps: number of steps rotated
```

#### Connectivity Events

```lua
-- Device comes online/offline
sse.connectivity("device-id", "connected", "action_name", {})
sse.connectivity("device-id", "disconnected", "action_name", {})
```

#### Light Change Events

```lua
-- React to light state changes
sse.light_change("light-id", "action_name", {
    resource_type = "light"  -- or "grouped_light", "*"
})
```

#### Wildcard Patterns

Use `*` or `|` for pattern matching:

```lua
sse.button("*", "short_release", "any_button", {})           -- any button
sse.button("id1|id2", "short_release", "some_buttons", {})   -- specific set
sse.connectivity("*", "connected", "any_connect", {})        -- any device
```

#### Unbinding Handlers

Dynamically change behavior at runtime:

```lua
sse.unbind_button("resource-id")                    -- remove all handlers
sse.unbind_button("resource-id", "short_release")   -- remove specific action
sse.unbind_rotary("resource-id")
sse.unbind_connectivity("device-id")
sse.unbind_light_change("resource-id")
```

#### SSE Configuration

```yaml
events:
  sse:
    enabled: true             # Set false to disable all SSE events
    min_retry_backoff: "1s"   # Initial delay after disconnect
    max_retry_backoff: "2m"   # Maximum delay (caps exponential growth)
    retry_multiplier: 2.0     # Backoff multiplier per retry
    max_reconnects: 0         # 0 = infinite, or limit attempts
```

When `enabled: false`, all `sse.button()`, `sse.rotary()`, `sse.connectivity()`, and `sse.light_change()` handlers will never trigger.

### Scheduler

The `sched` module provides time-based triggers with astronomical time support.

```lua
local sched = require("sched")
```

#### Schedule Definitions

```lua
sched.define(id, time_expr, action_name, args, opts)
```

| Parameter | Description |
|-----------|-------------|
| `id` | Unique schedule identifier |
| `time_expr` | Time expression (see below) |
| `action_name` | Action to run |
| `args` | Arguments table |
| `opts` | Options: `{ tag = "...", replay = true/false }` |

#### Time Expressions

**Fixed times:**
```lua
sched.define("morning", "07:00", "wake_up", {})
sched.define("night", "23:30", "sleep", {})
```

**Astronomical times:**
```lua
sched.define("dawn", "@dawn", "energize", {})
sched.define("sunrise", "@sunrise", "bright", {})
sched.define("noon", "@noon", "focus", {})
sched.define("sunset", "@sunset", "relax", {})
sched.define("dusk", "@dusk", "dim", {})
```

**With offsets:**
```lua
sched.define("pre_sunset", "@sunset - 30m", "prepare", {})
sched.define("post_sunrise", "@sunrise + 1h", "routine", {})
sched.define("late_morning", "@noon - 2h30m", "meeting", {})
```

#### Options

```lua
sched.define("scene:morning", "@dawn", "set_scene", { scene = "Energize" }, {
    tag = "scene_set",   -- group schedules for queries
    replay = true,       -- run on boot if missed (default: true)
})

-- replay = false means skip boot recovery for this schedule
sched.define("debug", "00:01", "print_status", {}, { replay = false })
```

#### Periodic Schedules

```lua
sched.periodic("heartbeat", "5m", "check_health", {})
sched.periodic("sync", "1h", "sync_state", {}, { tag = "maintenance" })
```

#### Querying Schedules

```lua
-- Run closest schedule (by time) for a tag
sched.run_closest({ tag = "scene_set", strategy = "NEXT" })  -- next upcoming
sched.run_closest({ tag = "scene_set", strategy = "PREV" })  -- most recent past

-- Get schedule info without running
local info = sched.get_closest({ tag = "scene_set", strategy = "PREV" })
if info then
    log.info("Current schedule: " .. info.id)  -- e.g., "scene:morning"
end

-- List schedules by tag (sorted by time)
local schedules = sched.list({ tag = "scene_set" })
for _, id in ipairs(schedules) do
    log.info("Schedule: " .. id)
end

-- Run specific schedule by ID
local ok, err = sched.run("scene:morning")
```

#### Disabling Schedules

```lua
sched.disable("schedule_id")
```

#### Printing Schedule

```lua
sched.print()                          -- today's schedule
sched.print({ format = "tomorrow" })   -- tomorrow's schedule
```

#### Scheduler Configuration

```yaml
events:
  scheduler:
    enabled: true             # Set false to disable all schedules
    geo:
      enabled: true           # Enable astronomical times (@sunrise, @sunset)
      use_cache: true         # Cache geocoded coordinates in SQLite
      name: "Amsterdam"       # City name for geocoding (uses Nominatim API)
      timezone: "Europe/Amsterdam"
      http_timeout: "10s"     # Timeout for geocoding requests
      # lat: 52.3676          # Optional: provide coords to skip geocoding
      # lon: 4.9041
```

When `scheduler.enabled: false`, `sched.define()` and `sched.periodic()` won't trigger. Astronomical times (`@sunrise`, `@sunset`, etc.) require `geo.enabled: true`.

### Webhooks

The `events.webhook` module exposes HTTP endpoints.
Webhook server will always return `200 OK` if request was accepted and `404 Not Found` if here is no webhook action for this path. You cannot return something as response body from your actions.



```lua
local webhook = require("events.webhook")
```

#### Defining Endpoints

```lua
webhook.define("POST", "/lights/toggle", "toggle_lights", {})
webhook.define("GET", "/status", "get_status", {})
webhook.define("PUT", "/scene/{name}", "set_scene", {})
```

#### Path Parameters

```lua
webhook.define("POST", "/group/{id}/scene/{scene}", "set_group_scene", {})

action.define("set_group_scene", function(ctx, args)
    local id = ctx.request.path_params.id
    local scene = ctx.request.path_params.scene
    
    local group, _ = hue.group(id)
    group:set_scene(scene)
end)
```

#### Request Body

```lua
action.define("handle_json", function(ctx, args)
    if ctx.request.json then
        local brightness = ctx.request.json.brightness
        local scene = ctx.request.json.scene
        -- use values
    end
end)
```

#### Webhook Configuration

```yaml
events:
  webhook:
    enabled: true           # Set false to disable webhook server
    host: "0.0.0.0"         # Bind address
    port: 8081              # HTTP server port
```

When `enabled: false`, the webhook HTTP server won't start and `webhook.define()` endpoints won't be accessible.

### Event Collection (Debouncing)

The `collect` module provides middleware for aggregating rapid events.

```lua
local collect = require("collect")
```

#### Double-Click Detection

```lua
-- Define a reducer that counts clicks
local function click_counter(events)
    return { click_count = #events }
end

-- Use collect.quiet: flush after Nms of no new events
sse.button("button-id", "short_release", "handle_click", {
    middleware = collect.quiet(350, click_counter)  -- 350ms window
})

action.define("handle_click", function(ctx, args)
    if args.click_count >= 2 then
        log.info("Double click!")
    else
        log.info("Single click")
    end
end)
```

#### Rotary Accumulation

```lua
-- Accumulate rotary steps during rapid rotation
local function rotary_accumulator(events)
    local total = 0
    for _, e in ipairs(events) do
        local sign = e.direction == "counter_clock_wise" and -1 or 1
        total = total + (e.steps or 0) * sign
    end
    return {
        direction = total >= 0 and "clock_wise" or "counter_clock_wise",
        steps = math.abs(total)
    }
end

sse.rotary("rotary-id", "adjust_brightness", {
    middleware = collect.quiet(80, rotary_accumulator)  -- 80ms quiet period
})
```

#### Collection Types

| Function | Description |
|----------|-------------|
| `collect.quiet(ms, reducer)` | Flush after `ms` of no new events |
| `collect.count(n, reducer)` | Flush after `n` events |
| `collect.interval(ms, reducer)` | Flush every `ms` |

#### Reducer Function

The reducer receives an array of events and returns a merged result:

```lua
local function my_reducer(events)
    -- events: array of event tables
    -- Return: table that becomes args for the action
    return { count = #events, first = events[1] }
end
```

---

## KV Storage

The `kv` module provides persistent key-value storage backed by SQLite.

```lua
local kv = require("kv")
```

#### Buckets

Data is organized into buckets:

```lua
-- Create/get a bucket
local scenes = kv:bucket("scenes", { persistent = true })   -- survives restarts
local cache = kv:bucket("cache", { persistent = false })    -- memory only
```

#### Operations

```lua
-- Store value
scenes:store("group_1", "Relax")

-- Store with TTL (seconds)
cache:store("temp", 42, { ttl = 300 })  -- expires in 5 minutes

-- Get value (nil if not found or expired)
local scene = scenes:get("group_1")

-- Check existence
if scenes:exists("group_1") then
    -- key exists
end

-- Delete key
scenes:delete("group_1")  -- returns true if deleted

-- List all keys
local keys = scenes:keys()
for _, key in ipairs(keys) do
    log.info("Key: " .. key)
end

-- Clear bucket
scenes:clear()
```

#### Bucket Management

```lua
-- Check if bucket exists
if kv:exists("scenes") then ... end

-- Delete entire bucket
kv:delete("scenes")

-- List all buckets
local buckets = kv:list()
```

#### KV Configuration

```yaml
kv:
  cleanup_interval: "5m"    # How often to remove expired keys (TTL cleanup)
```

---

## Utilities

### Logging

The `log` module provides structured logging:

```lua
local log = require("log")

log.debug("Debug message")
log.info("Info message")
log.warn("Warning message")
log.error("Error message")

-- With structured fields
log.info("Group toggled", { group = "1", action = "on" })
```

#### Logging Configuration

```yaml
log:
  level: "info"           # debug, info, warn, error
  use_json: false         # JSON format (recommended for production)
  colors: true            # Colorize text output (ignored when use_json=true)
```

### Utils

The `utils` module provides utility functions:

```lua
local utils = require("utils")

-- Sleep (blocks Lua execution)
utils.sleep(500)  -- milliseconds
```

### Geo

The `geo` module provides astronomical time calculations:

```lua
local geo = require("geo")

-- Get today's astronomical times (Unix timestamps)
local times = geo.today()
if times then
    local dawn = times.dawn
    local sunrise = times.sunrise
    local noon = times.noon
    local sunset = times.sunset
    local dusk = times.dusk
    local midnight = times.midnight
end

-- With custom location
local times = geo.today("New York")
```

---

## API Reference

### action

| Function | Signature | Description |
|----------|-----------|-------------|
| `define` | `action.define(name, fn)` | Register an action |
| `run` | `action.run(name, args)` | Run action immediately |

### sched

| Function | Signature | Description |
|----------|-----------|-------------|
| `define` | `sched.define(id, time_expr, action, args, opts)` | Define daily schedule |
| `periodic` | `sched.periodic(id, interval, action, args, opts)` | Define periodic schedule |
| `run_closest` | `sched.run_closest({tag, strategy})` | Run closest matching schedule |
| `get_closest` | `sched.get_closest({tag, strategy})` | Get closest without running |
| `list` | `sched.list({tag})` | List schedule IDs |
| `run` | `sched.run(id)` | Run schedule by ID |
| `disable` | `sched.disable(id)` | Remove schedule |
| `print` | `sched.print(opts)` | Print schedule to log |

### hue

| Function | Signature | Description |
|----------|-----------|-------------|
| `group` | `hue.group(id) -> (group, err)` | Get group object |
| `groups` | `hue.groups() -> (table, err)` | Get all groups |
| `light` | `hue.light(id) -> (light, err)` | Get light object |
| `lights` | `hue.lights() -> (table, err)` | Get all lights |

### hue.group / hue.light methods

| Method | Signature | Returns | Description |
|--------|-----------|---------|-------------|
| `id` | `:id()` | number | Resource ID |
| `name` | `:name()` | string | Resource name |
| `is_on` | `:is_on()` | bool | Power state |
| `any_on` | `:any_on()` | bool | Any light on (groups) |
| `all_on` | `:all_on()` | bool | All lights on (groups) |
| `get_bri` | `:get_bri()` | number | Current brightness |
| `get_state` | `:get_state()` | table | Full state |
| `on` | `:on()` | self | Turn on |
| `off` | `:off()` | self | Turn off |
| `toggle` | `:toggle()` | self | Toggle power |
| `set_bri` | `:set_bri(1-254)` | self | Set brightness |
| `set_scene` | `:set_scene(name)` | self | Activate scene |
| `set_color` | `:set_color(x, y)` | self | Set CIE xy color |
| `set_ct` | `:set_ct(153-500)` | self | Set color temp (mirek) |
| `set_hue` | `:set_hue(0-65535)` | self | Set hue |
| `set_sat` | `:set_sat(0-254)` | self | Set saturation |
| `alert` | `:alert(type)` | self | Flash light |
| `set_state` | `:set_state(tbl)` | self | Set multiple properties |

### events.sse

| Function | Signature | Description |
|----------|-----------|-------------|
| `button` | `sse.button(id, action, handler, args)` | Button handler |
| `rotary` | `sse.rotary(id, handler, args)` | Rotary handler |
| `connectivity` | `sse.connectivity(id, status, handler, args)` | Connectivity handler |
| `light_change` | `sse.light_change(id, handler, args)` | Light state handler |
| `unbind_button` | `sse.unbind_button(id, action?)` | Remove button handler |
| `unbind_rotary` | `sse.unbind_rotary(id)` | Remove rotary handler |
| `unbind_connectivity` | `sse.unbind_connectivity(id, status?)` | Remove connectivity handler |
| `unbind_light_change` | `sse.unbind_light_change(id, type?)` | Remove light handler |

### events.webhook

| Function | Signature | Description |
|----------|-----------|-------------|
| `define` | `webhook.define(method, path, handler, args)` | Define endpoint |

### kv

| Function | Signature | Description |
|----------|-----------|-------------|
| `bucket` | `kv:bucket(name, opts)` | Get/create bucket |
| `exists` | `kv:exists(name)` | Check bucket exists |
| `delete` | `kv:delete(name)` | Delete bucket |
| `list` | `kv:list()` | List bucket names |

### kv.bucket

| Method | Signature | Description |
|--------|-----------|-------------|
| `store` | `:store(key, value, opts?)` | Store value |
| `get` | `:get(key)` | Get value |
| `exists` | `:exists(key)` | Check key exists |
| `delete` | `:delete(key)` | Delete key |
| `keys` | `:keys()` | List keys |
| `clear` | `:clear()` | Clear all keys |

### collect

| Function | Signature | Description |
|----------|-----------|-------------|
| `quiet` | `collect.quiet(ms, reducer)` | Debounce by quiet period |
| `count` | `collect.count(n, reducer)` | Collect N events |
| `interval` | `collect.interval(ms, reducer)` | Collect over interval |

### log

| Function | Signature | Description |
|----------|-----------|-------------|
| `debug` | `log.debug(msg, fields?)` | Debug log |
| `info` | `log.info(msg, fields?)` | Info log |
| `warn` | `log.warn(msg, fields?)` | Warning log |
| `error` | `log.error(msg, fields?)` | Error log |

### utils

| Function | Signature | Description |
|----------|-----------|-------------|
| `sleep` | `utils.sleep(ms)` | Sleep for milliseconds |

### geo

| Function | Signature | Description |
|----------|-----------|-------------|
| `today` | `geo.today(location?)` | Get astronomical times |

### ctx (Action Context)

| Field/Method | Type | Description |
|--------------|------|-------------|
| `ctx.actual` | table | Actual state accessor |
| `ctx.desired` | table | Desired state builder |
| `ctx.request` | table/nil | HTTP request (webhooks) |
| `ctx:reconcile()` | function | Trigger reconciliation |
| `ctx:force_reconcile()` | function | Force full reconciliation |

### ctx.actual

| Method | Signature | Description |
|--------|-----------|-------------|
| `group` | `:group(id) -> ({all_on, any_on}, err)` | Get group state |

### ctx.desired:group / ctx.desired:light

| Method | Signature | Returns | Description |
|--------|-----------|---------|-------------|
| `on` | `:on()` | self | Set power on |
| `off` | `:off()` | self | Set power off |
| `toggle` | `:toggle()` | self | Toggle power |
| `set_scene` | `:set_scene(name)` | self | Set scene |
| `set_bri` | `:set_bri(1-254)` | self | Set brightness |
| `set_color` | `:set_color(x, y)` | self | Set CIE xy |
| `set_ct` | `:set_ct(153-500)` | self | Set color temp |
| `set_hue` | `:set_hue(0-65535)` | self | Set hue |
| `set_sat` | `:set_sat(0-254)` | self | Set saturation |

