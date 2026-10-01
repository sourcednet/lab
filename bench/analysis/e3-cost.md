# E3 · Storage cost

**Question:** what does keeping every version cost a publisher and a resolver?

**Setup:** `wikipedia-100`, replaying all 21 revisions of each article through a publisher and a resolver that syncs after each step. Results: `baseline`, `clean-extraction`, the Phase 8 `clean-small` reference, and `clean-small` with Phase 9's compact index (`20261001T071929Z`, the new reference).

## Numbers

| | baseline | clean-extraction | clean-small | **clean-small, Phase 9** |
| --- | --- | --- | --- | --- |
| Versions published (of 2,100 revisions) | 1,924 | 1,471 | 1,471 | 1,471 |
| Publisher files (records and bundles) | 822 MB | 249 MB | 273 MB | 273 MB |
| Resolver total | 2,006 MB | 547 MB | 571 MB | **109 MB** |
| per article (21 revisions) | 20.1 MB | 5.5 MB | 5.7 MB | **1.1 MB** |
| Resolver database | 1,947 MB | 532 MB | 557 MB | 95 MB |
| of which the passage index | most | most | most | 33 MB |
| of which the signed records | | | | 55 MB |
| Distinct chunk text, all versions | 58.8 MB | 14.6 MB | 13.9 MB | 13.9 MB |
| Deduplication (chunk uses per stored chunk) | 11.3× | 12.9× | 13.6× | 13.4× |
| Index windows (6 words) | 7.5 M | 2.0 M | 1.9 M | 1.9 M |
| Bundles the resolver downloaded | 1,718 | 1,270 | 1,265 | 1,265 |

First version only, `clean-small`: 12.3 MB of chunk text and a 368 MB database for 100 pages; with Phase 9, a **37 MB** database.

With the winnowed passage index (Phase 10, `20261001T100416Z-clean-small-winnowed`): resolver 71.6 MB instead of 89.0 MB, index 11.3 MB instead of 28.7 MB, at a small cost in quote lookups (E6).

Leaving out link-list sections too (`20261001T091459Z-clean-small`, see E1) cuts further: resolver **89 MB** (index 29 MB, records 43 MB, text 12 MB), publisher 214 MB, and 1,370 versions instead of 1,471. First version only: a 31 MB database and 10.4 MB of text.

Metrics: `cost.*`, with per-step detail in `cost.steps`. The index and record sizes (`cost.resolver_index_bytes`, `cost.resolver_record_bytes`, from SQLite's `dbstat`) are new in Phase 9. Deduplication now counts a chunk once per record that holds it, even if the record lists it twice, hence 13.4× instead of 13.6×.

## Conclusion

- **The passage index dominated resolver storage, and the cause was its layout.** The database, almost all of it the index, was 97% of the total, about 40× the distinct signed text. Each 6-word window was a row holding the full 71-character chunk ID as text, stored twice (the table and a second index): about 215 bytes per window.
- **Phase 9 numbers the chunks.** A window is now a hash and a chunk number, in one table (about 17 bytes); purging a chunk finds its windows by hashing its text again, so no second index is needed. The table of which records hold which chunks got the same change (about 340 bytes per entry before, about 10 after). Resolver storage fell 5.3× (571 → 109 MB), with the same windows and the same E6 lookup results. The index is now 31% of the resolver and 2.4× the text; one window per word can't get much smaller (fewer windows were tried: see E6).
- **The signed records are now the largest part** (55 MB, half the resolver). They are kept as the publisher served them, so they can be checked and served again. Each lists every chunk ID of its page, so 1,471 versions of long pages add up. Compressing them, or chunk proofs (Future Versions), would shrink them.
- **Content addressing works.** Every chunk is stored once across versions (11–14×), so a new version costs the resolver about 40 KB of database since Phase 9 (140 KB before), and very little text.
- **Bundles repeat unchanged chunks.** Publisher files are 273 MB for 14 MB of distinct text (about 19×). An edit adds 1.7 new chunks on average out of about 180 (E1), so a thin bundle would be about 1% of a full one. For 206 of the 1,471 versions the resolver already held every chunk and downloaded no bundle.
- **Thin bundles can wait.** A full bundle is a static file of a few hundred KB per version, which is cheap to host and to fetch once. The spec keeps full bundles in v1; thin bundles stay a v2 item.
- **Clean extraction cuts everything by 3.5–4×.** Part of that is fewer versions: edits that only touch references produce none.
