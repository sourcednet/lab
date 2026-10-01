# E4 · Latency

**Question:** how long does a verified fetch take, directly from the publisher and through a resolver, cold and warm?

**Setup:** `timing` result (and `timing-clean-small` for Phase 9, below): in-process fake network with a simulated 50 ms publisher round trip, 20 samples each. Baseline settings (300/1500/4000, everything signed). Timed results vary between runs and machines.

## Numbers

| Path | Median | p10 | p90 |
| --- | --- | --- | --- |
| Direct (validating client), cold | 212 ms | 209 ms | 216 ms |
| Direct, warm (`keys.json` cached) | 160 ms | 156 ms | 164 ms |
| Resolver, warm (page already held) | **34 ms** | 19 ms | 48 ms |
| Resolver, cold (first request for the page) | **920 ms** | 538 ms | 1,146 ms |

Metrics: `latency.*_ms`.

### Phase 9: indexing after answering

A new config, `timing-clean-small`, measures the chosen settings (clean extraction, 150/800/2000); `timing` stays pinned to the baseline. The old and new code ran back to back on the same machine: `20261001T071731Z` (commit `a25d4e7`, before) and `20261001T071824Z` (Phase 9).

| Path | Before | Phase 9 |
| --- | --- | --- |
| Direct, cold | 216 ms | 218 ms |
| Resolver, cold | **489 ms** (p10 312, p90 689) | **194 ms** (p10 177, p90 235) |
| Resolver, warm | 15 ms | 14 ms |
| Resolver, warm after indexing (new metric) | – | 17 ms |

The resolver now stores a new page's text, answers, and indexes it in the background about 0.1 s later. Indexing one page takes about 120 ms (7 µs per window).

## Conclusion

- **Direct verification costs about one round trip per file:** keys, page link, record, and bundle, then 3 once keys are cached. This matches the spec's "3 small requests".
- **A warm resolver is fast:** it answers from its store within the freshness window, with no request to the publisher.
- **A cold resolver was slow, and the cause was ours.** The publisher round trips account for about 200 ms; the rest was indexing the page's chunks before answering (about 40,000 windows on baseline settings, 17,000 on clean-small). Since Phase 9, a cold fetch costs the round trips and little else: 194 ms, about the same as verifying directly.
- **How the indexer stays out of the way** (Phase 9): new text is queued and indexed about 0.1 s after the request that brought it in, 4 chunks per transaction on the store's one database connection, so a request waits at most a few milliseconds for it. Two things were tried and dropped: indexing at once (the request right after a cold fetch took about 40 ms instead of 14), and giving the indexer its own connection (SQLite's lock retries kept requests waiting until a whole page was indexed). A passage lookup first indexes whatever is still queued, so it never misses text.
- **Not measured:** real networks and TLS. The relative picture (cache hit, a few round trips, indexing) should hold.
