# E1 · Chunk stability

**Question:** do citations (chunk IDs) survive real edits?

**Setup:** `wikipedia-100`, 1,997 consecutive edits. Results: `baseline`, `clean-extraction`, `small-chunks`, `medium-chunks`, `large-chunks`, and the `clean-small` reference. "Survival over 20 edits" is the share of a page's first-version chunk IDs still present in its last version. "Per edit" counts only edits that changed the signed text.

## Numbers

| Config | Chunking | Survive 20 edits, median (p10) | Per edit, mean | Edits that don't change signed text | New chunks per edit, mean (p90) |
| --- | --- | --- | --- | --- | --- |
| baseline | 300/1500/4000 | 63.2% (41.5%) | 95.0% | 8.6% | 15.0 (42) |
| small-chunks | 150/800/2000 | 67.5% (46.3%) | 95.7% | 8.6% | 21.3 (61) |
| medium-chunks | 200/1000/3000 | 66.1% (45.9%) | 95.5% | 8.6% | 18.4 (54) |
| large-chunks | 600/3000/6000 | 55.4% (32.3%) | 93.8% | 8.6% | 10.8 (30) |
| clean-extraction | 300/1500/4000 | 92.4% (77.9%) | 98.5% | 31.1% | 1.6 (3) |
| **clean-small** | 150/800/2000 | **93.9% (83.2%)** | **98.9%** | 31.1% | 1.7 (3) |

Metrics: `stability.survival_first_to_last`, `survival_per_edit`, `unchanged_share`, `new_chunks_per_edit`.

### Phase 9: link-list sections left out

Since the first E7 session, clean extraction also leaves out the See also, External links, Further reading, References, and Bibliography sections (`extraction.drop_sections`). Result `20261001T091459Z-clean-small` against `20261001T071929Z-clean-small`:

- Citations surviving 20 edits: median 93.9% → 93.5%, per edit 99.25% → 99.08%. The dropped lists rarely changed, so they had been propping up survival; no citation got less stable.
- Edits that don't change the signed text: 31% → **36%**, since edits that touch only those sections no longer make a version.

## Conclusion

- **Extraction matters far more than chunk size.** Without it, the typical edit changes 1 chunk, but one edit in ten changes 42 or more. Inserting one reference renumbers every later `[n]` marker, so every later chunk changes. Leaving out reference lists and markers fixes that: the p90 drops from 42 new chunks per edit to 3.
- **A third of Wikipedia edits only touch references or navigation.** With clean extraction they no longer produce a new version at all.
- **Smaller chunks survive better,** because an edit invalidates less text. Large chunks are worst on every measure.
- **Spec:** settled the 150/800/2,000 sizes, plus guidance to sign content rather than chrome and to leave out automatic numbering (done Oct 1).
- **What's left:** the worst article still keeps only 66% after 20 edits, from heavy rewriting. A citation that no longer survives isn't lost: `resolve` reports the change and offers the passage's latest version.
