# Lightd

Lightd is a standalone Philips Hue automation daemon: Go manages event delivery, SQLite and device reconciliation; a single Lua worker runs your automation. It uses the local Hue V2 API through OpenHue and receives button, rotary and device events through SSE.

**v0.3 is a forward-only API update.** Read [migration instructions](docs/MIGRATING_0.3.md) before replacing an older image or script. Native resource UUIDs, percentage brightness and millisecond transitions replace the V1-shaped controls.

## What it does

- Reacts to Hue buttons, rotary controls, connectivity changes, HTTP webhooks and schedules.
- Supports direct light/group control, CT/XY/HSV color, scenes, transitions, per-member snapshots, and device-supported effects/signals.
- Persists desired state and applies it with bounded retries. Newer manual or desired commands supersede older work on overlapping lamps.
- Provides fixed/solar schedules and declarative seasonal policies with one shared evaluator for execution and offline preview.
- Records admission, execution and outcomes independently of deduplication. Manual and nested actions are visible too.

The daemon does not replay arbitrary interrupted Lua callbacks after a crash. Startup can explicitly reassert the latest configured lighting phase. A completed Lua action and a bridge acknowledgment are separate from physical lamp observation.

## A small automation

```lua
local action = require("action")
local hue = require("hue")
local webhook = require("events.webhook")

-- Replace this synthetic UUID with a room or zone UUID from your bridge.
local room = "10000000-0000-0000-0000-000000000001"

action.define("toggle", function(ctx, args)
    local lights, err = hue.group(room)
    if err then error(err) end
    lights:toggle()
end)
webhook.define("POST", "/toggle", "toggle", {})

action.define("evening", function(ctx, args)
    ctx.desired:group(room):preserve_power():set_scene("Relax")
end)
```

Desired changes commit automatically when the action succeeds. `ctx:reconcile()` explicitly commits a checkpoint earlier. Failed actions discard only their uncommitted desired changes; prior direct commands, KV writes and completed child actions remain visible effects.

See [native Hue controls](docs/HUE_API.md), the [Lua manual](MANUAL.md), and [calendar policies](docs/POLICY.md) for the full interface.

## Run with Docker

Create a private `.env` containing `HUE_BRIDGE` and `HUE_TOKEN`, and a `lightd.lua` script. Do not commit these files.

```yaml
services:
  lightd:
    image: ghcr.io/dokzlo13/lightd:0.3.1
    restart: unless-stopped
    env_file: .env
    environment:
      TZ: UTC
      GEO_ENABLED: "false"
    volumes:
      - ./data:/app/data
      - ./lightd.lua:/app/config/lightd.lua:ro
    ports:
      - "8080:8080"
      - "9090:9090"
```

The image runs as UID 1000. Give that user write access to the data directory. A read-only root filesystem is supported with writable `/app/data` and `/tmp` mounts. SQLite requires a single active daemon per database.

Configuration examples are [config.example.yaml](config.example.yaml) and [config.docker.yaml](config.docker.yaml). Unknown configuration fields fail validation. Enable solar scheduling by supplying your private location settings and IANA timezone. The image validates Hue certificate chains with local client configuration; it never changes global HTTP defaults.

## Check and preview without controlling lights

```sh
lightd --version
lightd --check-config -c config.yaml

# Synthetic coordinates, for demonstration and CI only.
lightd --preview-policy examples/seasonal-policy.lua \
  --date 2026-12-21 --timezone UTC --latitude 0 --longitude 0 --json
```

Config checking parses configuration and compiles automation Lua syntax without executing it. It cannot verify arbitrary dynamic action registration; normal startup checks references after loading. Preview accepts a separate literal policy file and does not open the bridge, database or normal automation script. Its location inputs are explicit; it does not geocode.

The [seasonal example](examples/seasonal-policy.lua) uses solar/day-length timing with local evening bounds. It is an example, not a built-in change to anyone's schedule. Mount the policy beside your script and register it with:

```lua
local sched = require("sched")
sched.policy("lighting", dofile("/app/config/lighting-policy.lua"))
```

Define the policy's referenced actions in your automation script. Fixed times in a DST gap are skipped; repeated times run once at the first occurrence. Missing solar events remain unavailable unless a fallback is configured. Same-time unrelated schedules all receive admission attempts in a defined order.

## Operational behavior

`/health` reports process liveness. `/ready` and `/status` report initialization, Lua/SQLite admission health, bridge/SSE state and reconciliation status. A bridge outage is visible as dependency degradation and does not itself create a liveness restart loop.

Webhooks return `202` with an ingress receipt after accountable acceptance; `404` means no route, `429` means overload and `503` means unavailable. Accepted work is bounded and may be interrupted; a receipt is not a promise to replay a side effect after restart. Reducer failures never become empty default-command arguments.

Default action execution is bounded to 30 seconds, Hue requests to five seconds and total shutdown to ten seconds. Explicit configuration overrides remain supported. Desired retries back off from one to 30 seconds and stop active sending after ten minutes; expired intent remains inspectable. Native effects are capability dependent; V1 `colorloop` and exact V1 HS state semantics have no compatibility shim.

`LEDGER_ENABLED=false` disables optional detailed history, not the minimal correctness records. Dedupe claims survive failures and interruptions for at least seven days; explicit reruns use new invocation IDs. Interrupted outcomes and active delivery records are retained rather than silently forgotten.

## Build, test and release

Go 1.27.3 and a C compiler are required for the bundled SQLite driver.

```sh
./scripts/check.sh
go build -tags sqlite_omit_load_extension -trimpath -o lightd ./cmd/lightd
```

Checks include race tests, SQLite fault/migration cases, fake Hue/SSE protocol tests, full-service synthetic controls/restart tests and calendar simulations. No tests require a live bridge, home coordinates or credentials.

Tagged releases build native Linux amd64/arm64, macOS arm64 and Windows amd64 binaries, plus a Linux amd64 container. CI gates publication on tests, native SQLite/CLI smoke, container checks and vulnerability scanning. Archives include dependency notices, examples and migration documentation; releases include checksums and the immutable image digest. See [release procedure](docs/RELEASING.md).
