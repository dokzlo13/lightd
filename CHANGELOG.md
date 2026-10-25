# v0.3.1

A forward-only maintenance release for reliable Hue automation and seasonal policies.

The v0.3.0 tag was withheld from release publication by the Windows packaging gate. This tag adds explicit UTF-8 handling for cross-platform dependency notices; the automation implementation is unchanged.

- Adopt OpenHue's native V2 REST client and upstream SSE framing. Use UUIDs, percentage brightness and millisecond transitions; add per-member snapshots and explicit power-preserving scene application.
- Fix sparse false/zero fields, error envelopes, cancellation, scene refresh and reconnect behavior. Native color, effects and signals remain capability-aware; V1 `colorloop` and exact partial-HS/readback behavior are not emulated.
- Record action/ingress receipts independently of dedupe. Reserve execution capacity before acceptance, audit manual/nested/collected actions, and never automatically replay interrupted imperative effects.
- Commit successful desired batches and outcomes atomically. Isolate nested frames, preserve repeated builder changes, reject failed reducers and propagate persistence errors.
- Persist per-lamp command authority and delivery progress. Newer overlapping desired/direct commands supersede stale retries, including across restart. Imported historical desired state is not automatically enacted.
- Correct solar date/timezone conversion, DST day boundaries, simultaneous schedules, catch-up and periodic validation. Add atomic seasonal policies and restricted offline previews.
- Join workers and collectors within a total shutdown budget; expose meaningful readiness/status and strict configuration checks. Bound HTTP payloads, redirects and Lua table conversion.
- Update Go, SQLite and other dependencies; add release gates, native platform smoke, dependency notices, build identity, checksums and container provenance.

**Upgrade requires migrating the image, Lua/config and desired-state IDs/units together.** Back up SQLite first. Follow `docs/MIGRATING_0.3.md`; rollback restores the old image/config and backup. Arbitrary KV values are preserved. Bedtime anchors are configuration, not automatically changed by this release.
