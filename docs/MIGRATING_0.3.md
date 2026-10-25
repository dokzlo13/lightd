# Migrating to v0.3

This is a forward-only migration. Deploy the new image, Lua script/policy and database changes together. There is no V1 compatibility backend. Keep the old image/config and an online SQLite backup for rollback; an additive SQL schema alone does not make new desired-state JSON safe for the old binary.

## Prepare privately

1. Back up SQLite using SQLite's online backup API or an equivalent consistent snapshot. A plain copy of an active WAL database is not sufficient.
2. Export/read the bridge's V2 inventory. Match old `/groups/N` and `/lights/N` aliases to native room/zone/light UUIDs, and check scene ownership/names. Keep the inventory and resource mapping private, outside Git and CI.
3. Adapt the automation using [HUE_API.md](HUE_API.md). The full native API includes light/group control, scenes, CT/XY/paired HSV, transitions, snapshots, supported effects/signals and events. It is not limited to the included example script.
4. Validate the new script with `--check-config`; preview the separate pure policy file for representative dates. Config check does not execute top-level actions or prove dynamic registration correctness.

The mapping file has this shape, with your real values supplied privately:

```json
{"groups":{"OLD_GROUP_ID":"NATIVE_ZONE_UUID"},"lights":{"OLD_LIGHT_ID":"NATIVE_LIGHT_UUID"}}
```

## Script changes

| Previous form | Native form |
|---|---|
| Numeric group/light IDs | Native resource UUIDs; room/zone IDs identify groups |
| `set_bri(n)` / `get_bri()` | `set_brightness(n*100/254)` / `get_brightness()` |
| `set_state({bri=...,transitiontime=...})` | `set_state({brightness=...,duration=...})`, milliseconds |
| Desired brightness/transition | `set_brightness(percent)` / `set_transition(ms)` |
| Saved group `{ct,xy,colormode,bri}` | `snapshot()` / `restore(snapshot)` per member |
| Partial native hue/sat setters | Paired `set_hsv(degrees,saturation_percent)` or XY |
| V1 alert/effect names | Native supported `signal(name,duration_ms)` / `effect(name)` |
| Automatic scene phase | `ctx.desired:group(id):preserve_power():set_scene(scene)` |
| Explicit scene activation with power | `group:set_scene(scene,true,duration_ms)` or desired `on():set_scene(scene)` |

Zero brightness is minimum brightness, not off; use `off()` for power. Appearance-only writes preserve off members. Group brightness is the mean of currently-on members, not a cached V1 group-action value. `set_scene(scene,true)` deliberately powers on selected scene members; it does not reproduce a scene's saved per-member off choices. Default scene application strips power.

Setters/getters raise Lua errors on failure; resource factories retain `(object,error)` results. Return normally only when the action should be recorded as successful. Use `error(err)`/`assert` or `pcall` deliberately. `action.run` is audited, including nested calls. Children have independent desired buffers and commit boundaries.

The old `colorloop` literal and exact V1 HS readback have no guaranteed native equivalent. Unsupported values fail explicitly. Effects/signals depend on each target's capabilities; the union of available effects across a bridge is not a guarantee for every lamp.

Keep schedule phase IDs stable when manual cycling stores positions/IDs. The seasonal policy example retains 22:45 Rest and 23:50 Nightlight; choose any different bedtime anchors explicitly. Mount the pure policy as a second file and register it with `sched.policy("lighting",dofile("/app/config/lighting-policy.lua"))`.

## Offline desired-state migration

Stop lightd before applying the migration. Test against a backup copy first:

```sh
python3 scripts/migrate-hue-v2.py --database COPY.sqlite --mapping PRIVATE-MAPPING.json --scene-bucket scenes
python3 scripts/migrate-hue-v2.py --database DATA.sqlite --mapping PRIVATE-MAPPING.json --scene-bucket scenes --apply
```

Without `--apply`, changes roll back. The tool converts mapped desired resource IDs, brightness and transition units atomically; rejects collisions/unmapped IDs and unsupported old HS/effect payloads; and remaps only keys in the explicitly named script-owned scene bucket. Other user KV data stays opaque. It clears authority/delivery journals only when changes were actually made. Do not run it against a live database.

Existing ledger/history/KV tables remain. On first startup, historical desired rows without delivery journals become **imported** baselines; they do not enact old power settings. The new boot-policy invocation or a new/forced desired command publishes current authority. Ordinary restarts preserve `hue_target_authority_v1` and `hue_delivery_v1` so old retries cannot undo newer manual commands. `--reset-state` clears desired state and these journals explicitly.

## Delivery and operations changes

- HTTP webhook acceptance is `202` with an ingress receipt; missing route is 404, overload 429 and unavailable 503. Collection has bounded buffers and a recorded disposition.
- Accepted action records exist before callbacks; running work gets a durable start. Same-key retries do not rerun completed, failed or interrupted callbacks. Explicit reruns get a new ID. A crash does not replay arbitrary toggles, HTTP calls or relative adjustments.
- Implicit desired writes commit atomically with successful action outcome. Failed actions discard uncommitted patches. Earlier explicit `ctx:reconcile()` checkpoints, direct calls, KV writes and child commits remain effects.
- `LEDGER_ENABLED=false` controls optional detailed history only. Minimal admission/dedupe/delivery records remain. Terminal dedupe retention is at least seven days; active/uncertain records are retained.
- Startup checks action references, listener failures fail startup, and worker failures cause a nonzero exit. Unknown config fields, invalid intervals and unsupported payloads now fail visibly.
- Default action budget 30s, Hue request timeout 5s and shutdown budget 10s. Explicit existing overrides remain. Desired retries are 1s through 30s, with ten-minute active expiry and 30s total attempt budget.
- `/health` is liveness; `/ready` and `/status` expose admission/storage/worker health and separate bridge/SSE/retry state. Bridge outage alone does not cause a liveness restart loop.

After rollout verify build version/commit, expected resource identities, readiness, SSE connection and action/reconciliation status. Observe actual power/appearance with read-only bridge checks; no automated rollout test should turn on an off lamp. Full synthetic controls tests accompany the release, but simulation does not prove every device's physical response.

Rollback stops the new daemon, restores the consistent pre-upgrade database and old Lua/config, and deploys the previous immutable image digest. Do not merely change the image tag while leaving new state/script formats in place.
