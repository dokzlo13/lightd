# Calendar policies and offline validation

Lightd evaluates daily schedules in the configured scheduler timezone. The process `TZ` does not change solar instants or calendar dates. Solar timestamps retain their real dates: summer civil dusk can belong to the following date. An absent polar crossing stays absent.

A fixed wall time in a spring DST gap is skipped. A time repeated in an autumn fold runs at its first occurrence once. Calendar previews use local midnight through the next local midnight, including 23-hour and 25-hour days.

## Offline commands

```sh
lightd --check-config --config config.yaml
lightd --preview-policy examples/seasonal-policy.lua \
  --date 2026-12-21 --timezone UTC \
  --latitude 0 --longitude 0 --json
```

`--check-config` validates effective configuration and syntax-compiles ordinary automation Lua. It does not run the script or claim that dynamic action registrations are valid. Normal startup validates schedule action references after the script loads.

`--preview-policy` reads a separate pure policy file. It does not initialize Hue, SQLite, network clients, actions, or the ordinary automation script. The file must contain only `return { ... }` with literal tables, strings, numbers, booleans and nil. Negative numeric literals are allowed. Calls, functions, loops, local definitions, concatenation and other expressions are rejected before execution. The source limit is 1 MiB, and nested table conversion has depth and entry limits. A separate VM has no libraries or application modules.

The JSON result includes policy ID/version, local date/timezone, solar inputs, phase rank, actual timestamp with offset, raw anchor, curve weight and clamp/fallback/skip reasons. A timestamp means a planned occurrence, not confirmed execution or physical lamp state. The normal printed schedule labels times as `past` or `future`, and caps output at 256 entries. Periodic schedules continue running beyond this display limit.

## Installing a policy

The example preserves the existing six phase IDs and the unmodified bedtime anchors of 22:45 and 23:50. It moves the warm phase between 17:00 and 20:30, starts Read one hour earlier, and adds a gradual day-length-dependent offset. Those values are configurable preferences. The calendar only sets phase times; it does not interpolate scene colors continuously.

In the ordinary automation script:

```lua
local sched = require("sched")
local policy = dofile("/app/config/seasonal-policy.lua")
sched.policy(policy.id, policy)
```

Define `store_scene_all` in that automation script using the current desired-state API. Automatic phase updates should explicitly preserve each member's power. The pure policy file contains only action names and scene arguments; it never controls lights by itself. Remove the six old individual `sched.define` calls before installing the policy, because a phase ID cannot belong to two definitions. Existing `sched.list`, `get_closest`, `run` and `run_closest` continue to work with the phase IDs.

`sched.policy(id, spec)` validates the entire policy before replacing its prior generation atomically. A failed replacement leaves the prior policy installed. It checks 366 consecutive dates before installation and validates every evaluated date at runtime. Queued work from an older generation ends with a superseded error before its action executes. The scheduler allows up to 256 definitions. A policy has 1–64 phases, and its `version` is recorded with action provenance. Change the version when changing its meaning.

A complete policy can be disabled with `sched.disable(policy.id)`. Individual phase replacement or disable is rejected; replace the complete policy to change its members. Independent daily and periodic schedules retain their existing registration APIs.

Live structured preview:

```lua
local preview, err = sched.preview({policy="lighting", date="2026-12-21"})
```

It returns date, timezone, version and phase entries with ID, action, skipped flag, Unix `time` and RFC3339 `timestamp`. The CLI exposes the fuller explanations. `geo.for_date("2026-12-21")` returns the same solar timestamps used by the evaluator; `geo.today()` remains available. Missing solar fields are nil.

## Timing rules

Each phase requires `id` and `action`, optional `args`, and exactly one anchor:

- `at`: an existing `HH:MM`, `@dawn`, `@sunrise`, `@noon`, `@sunset` or `@dusk` expression, optionally with a solar duration offset.
- `relative_to`: another phase ID in the same policy. References may point forward in declaration order, but must be acyclic.

Optional fields apply in this order:

1. `offset`: elapsed duration added to the anchor.
2. `offset_curve`: a bounded duration interpolated from the day's sunrise-to-sunset length.
3. `fallback`: fixed `HH:MM` used if the anchor, reference or curve input is unavailable.
4. `earliest` and `latest`: local fixed-time bounds on the anchor's calendar date.

A nonexistent fixed fallback or bound is also skipped with an explanation. A missing horizon event makes day length unavailable, including during polar day/night; no 0/24-hour approximation is silently substituted.

```lua
offset_curve = {
    input = "day_length",
    from = "9h20m6s", to = "6h",
    start = "0m", finish = "1h",
    easing = "smoothstep", -- or "linear"
}
```

The normalized input is `u = clamp((day_length - from) / (to - from), 0, 1)`. Smoothstep changes it to `u*u*(3-2*u)`. The output duration runs from `start` to `finish`. Descending input endpoints are supported; equal or negative day-length endpoints are rejected. Each individual offset is bounded to 24 hours, and summed phase displacement is bounded to 48 hours. Actual occurrence timestamps are rounded down to seconds.

Phases must remain in nondecreasing declared order for every evaluated date. Equal-time phases within a policy use declared rank. Independent schedules use a stable ID order. Ordinary simultaneous schedules all dispatch; normal simultaneous policy phases dispatch in rank order. When a policy has missed multiple different phase times, only its latest effective time is applied; phases sharing that time still follow declared rank. Boot/current-phase queries choose the last declared phase when times tie.

## Admission, restart and manual semantics

The scheduler persists a cursor independently of optional action-history cleanup. It advances only after downstream Lua admission has recorded a receipt, or after an explicitly logged expiration. Queue rejection retains the occurrence for retry. Receipt admission is not action success or lighting convergence.

Independent schedules catch up within five minutes, at most 256 occurrences per dispatch batch; older occurrences expire visibly. Policy catch-up selects the latest effective phase instead of replaying obsolete phases. A process restart explicitly reasserts the latest eligible schedule per tag, using a unique boot invocation identity while preserving the original planned time. Untagged definitions recover independently; `replay=false` and periodic schedules skip boot replay. Startup then resets scheduling cursors to the recovery boundary.

Manual `sched.run` and `run_closest` remain repeatable. They create independently audited invocations without consuming the future scheduled occurrence. Missing schedules, invalid strategies and rejected admission return errors. Action names are validated after startup registration so defining actions after their schedules remains supported.

Arbitrary external effects cannot be exactly once across a crash. Old accepted work is accounted for by the invocation subsystem, not blindly replayed. Explicit current-phase boot recovery can replace a manual phase choice; continuous fades and persisted manual override lifetimes are separate capabilities.

## Validation coverage

Regression tests cover both Helsinki DST transitions, process-zone independence, cross-midnight offsets, polar absence and fallback, 366-day phase/search/preview parity, queue rejection and persisted cursor behavior, simultaneous deadlines, bounded catch-up, definition supersession, policy rejection/atomicity, pure-loader effect rejection, and bounded previews. A generic Helsinki solar fixture is checked against the [US Naval Observatory API](https://aa.usno.navy.mil/data/api) with a three-minute tolerance for the local approximation and minute-rounded reference. None of these tests contact the live bridge.
