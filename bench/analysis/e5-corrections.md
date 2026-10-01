# E5 · Correction speed

**Question:** after a publisher issues a correction, how soon does a resolver serve it?

**Setup:** `timing` result: 10 corrections on the fake network. The resolver polls every 2 s, with and without the publisher sending `announce`. Timed results vary between runs.

## Numbers

| Path | Median | Min | Max |
| --- | --- | --- | --- |
| Polling only (2 s interval) | 1,207 ms | 1,159 ms | 1,230 ms |
| With announce | **95 ms** | 17 ms | 150 ms |
| `resolve` (checks the publisher live) | at once | | |

Metrics: `corrections.polling_only_ms`, `with_announce_ms`.

On the chosen settings, with Phase 9's background indexing (`20261001T071824Z-timing-clean-small`): 1,385 ms polling only, **81 ms** with announce (82 ms before Phase 9, in `20261001T071731Z`). Indexing after answering doesn't slow corrections down.

## Conclusion

- **Announce turns "at the next poll" into about 0.1 s.** Polling averages half the interval plus the sync. A 2 s interval is a test setting; real resolvers poll every few minutes or hours, so without announce a correction waits that long.
- **`resolve` never waits:** it asks the publisher live, so an app checking a citation it already holds always sees the correction.
- **Spec:** publisher tooling should announce (added Oct 1). Announces are hints, rate-limited per domain.
