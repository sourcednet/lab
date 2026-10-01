# E6 · Passage lookup

**Question:** given a quote and no URL, does the resolver find which signed page it came from?

**Setup:** 300 sampled sentences per config (3 per article, fixed seed), in three forms: the exact sentence, a fragment of it, and the sentence with one word changed. The resolver's index is 6-word windows. "Ambiguous" means more than one page matched equally well.

## Numbers

| Config | Exact: found / top match | Fragment: found / top match | Edited: found / top match | Ambiguous |
| --- | --- | --- | --- | --- |
| baseline | 100% / 99.7% | 99.7% / 97.7% | 100% / 99.7% | 5.3% |
| small-chunks | 100% / 99.7% | 99.7% / 98.3% | 100% / 99.7% | 3.3% |
| large-chunks | 100% / 100% | 100% / 99.7% | 99.7% / 99.7% | 2.3% |
| clean-extraction | 100% / 99.7% | 100% / 99.3% | 100% / 99.7% | 0.3% |
| clean-small | 100% / 100% | 99.7% / 99.7% | 100% / 100% | 0% |
| **clean-small, Phase 9** (compact index) | 100% / 100% | **100% / 100%** | 100% / 100% | **0%** |

Edited sentences are found as partial matches (72–82% word overlap), never as exact matches, as intended: a changed quote is not the signed text.

The Phase 9 row (`20261001T071929Z-clean-small`) is the compact index: the same windows, stored as numbers. Its one gained fragment comes from the extraction fix that separates table cells, not from the index. With link-list sections left out as well (`20261001T091459Z-clean-small`), lookups stay at 99.7–100%, with 2 of 300 best matches ambiguous (0.7%); the samples differ, since the pages' chunks changed.

**Fewer windows (Phase 9, then Phase 10):** keeping only the smallest hash of every 4 consecutive windows (winnowing) stores 60% fewer windows. Phase 9 tried it and kept all windows as the default. Phase 10 made it the second passage index (`index: winnowed`, `-index winnowed`), for resolvers short on space, with lookups that score every candidate on its full text:

| `20261001T091459Z-clean-small` vs `20261001T100416Z-clean-small-winnowed` | windows (default) | winnowed |
| --- | --- | --- |
| Exact sentences found | 100% | 100% |
| Fragments found | 99.7% | 97.3% |
| Edited sentences found | 100% | 97.7% |
| Ambiguous | 0.7% | 0.7% |
| Index | 28.7 MB | 11.3 MB |
| Resolver total | 89.0 MB | 71.6 MB |

Winnowing always finds passages of 9 words or more; shorter fragments are found only if they happen to hold a stored window.

Metrics: `passages.{exact_sentence,fragment,edited_sentence}.*`.

## Conclusion

- **Lookup works.** Exact and edited quotes are found essentially always, and fragments 99.7–100%.
- **Ambiguity came from boilerplate.** Reference entries and navigation repeat across articles; clean extraction removed nearly all of it.
- **The cost was storage, not accuracy.** Phase 9 stored each window as two numbers (a hash and a chunk number, about 17 bytes) instead of the 71-character chunk ID twice (about 215 bytes). Lookups are unchanged, and the index fell from about 97% of the resolver to about a third (E3).
- **Spec open question settled:** how to normalize and window passages stays the resolver's choice. Every match is checked against signed chunks, so resolvers need to agree only on the answer, not on the index.
