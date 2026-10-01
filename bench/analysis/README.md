# Analysis (Phase 8)

One short note per experiment, written from the results in `bench/results/`. The notes feed the spec (open questions, wording) and the pitch (tokens saved, citation accuracy, correction speed, cost per 1,000 pages). The task list is Phase 8 in `Dev/TODO.md`.

## Note format

Each note is a Markdown file in this folder, with four parts:

1. **Question:** what the experiment asks, in one sentence.
2. **Setup:** corpus, configs compared, and the result files used.
3. **Numbers:** a small table, with the metric names as they appear in the result JSON.
4. **Conclusion:** what it means for the spec or the resolver, and what stays open.

Notes: [`summary.md`](summary.md) (the pitch numbers), [`e1-stability.md`](e1-stability.md), [`e2-tokens.md`](e2-tokens.md), [`e3-cost.md`](e3-cost.md), [`e4-latency.md`](e4-latency.md), [`e5-corrections.md`](e5-corrections.md), [`e6-passages.md`](e6-passages.md), and [`ranking-search.md`](ranking-search.md). `e7-citations.md` follows once E7 is scored by hand.

## Results

Corpus: `wikipedia-100` (100 articles, 21 revisions each, 1,997 edits). Every result records the corpus checksum and git commit.

| Result | Chunking | Extraction | Experiments | Commit |
| --- | --- | --- | --- | --- |
| `20261001T031519Z-baseline` | 300/1500/4000 | everything | stability, tokens, cost, passages, ranking | `8a3c8b1` (dirty) |
| `20261001T032740Z-clean-extraction` | 300/1500/4000 | clean | same | `8a3c8b1` (dirty) |
| `20261001T033301Z-small-chunks` | 150/800/2000 | everything | stability, tokens, passages, ranking | `8a3c8b1` (dirty) |
| `20261001T033918Z-medium-chunks` | 200/1000/3000 | everything | same | `8a3c8b1` (dirty) |
| `20261001T034626Z-large-chunks` | 600/3000/6000 | everything | same | `8a3c8b1` (dirty) |
| `20261001T035421Z-timing` | 300/1500/4000 | everything | latency, corrections | `8a3c8b1` (dirty) |
| `20261001T041026Z-clean-small` | 150/800/2000 | clean | stability, tokens, cost, passages, ranking | `06f4d95` |
| `20261001T053546Z-clean-small` (Phase 8 reference) | 150/800/2000 | clean | same, plus search | `f14a3f1` |
| `20261001T071731Z-timing-clean-small` | 150/800/2000 | clean | latency, corrections (before Phase 9) | `a25d4e7` |
| `20261001T071824Z-timing-clean-small` | 150/800/2000 | clean | latency, corrections | `a25d4e7` (dirty: Phase 9) |
| `20261001T071307Z-clean-small-lead` | 150/800/2000 | clean | tokens, ranking, with the `bm25-lead` ranker | `a25d4e7` (dirty: Phase 9) |
| `20261001T071929Z-clean-small` | 150/800/2000 | clean | stability, tokens, cost, passages, ranking (with a held-out set and paging) | `a25d4e7` (dirty: Phase 9) |
| **`20261001T091459Z-clean-small`** (reference) | 150/800/2000 | clean, link-list sections dropped | same; search returns 3 passages for its first result | `d24b9a7` (dirty) |
| `20261001T092239Z-clean-small-lead` | 150/800/2000 | same | tokens, ranking, with `bm25-lead` | `d24b9a7` (dirty) |
| `20261001T100416Z-clean-small-winnowed` | 150/800/2000 | same | cost, passages, with the `winnowed` passage index | `67356ae` (dirty: Phase 10) |

"Clean" is `bench.WikipediaDrop`: no reference lists, citation markers, navigation boxes, hatnotes, or images.

The reference result is the chosen settings with Phase 9's resolver: compact passage index, indexing after answering, the default `bm25` ranker, table cells kept apart, link-list sections (See also, External links, …) left out, and three passages for search's first result. It ran on uncommitted code; rerun it after committing, to record the commit. `20261001T071929Z` is the step before the last two changes, made after the first E7 session. Phase 10's refactoring (ranker and passage index behind interfaces) was checked by rerunning the reference: every metric matched. Two runs on the same code matched exactly, and the Phase 8 reference matched an earlier run across commits, so the numbers should hold.

Phase 9 compared against the Phase 8 reference: storage fell 5.3× (E3); stability, tokens, and ranking with `bm25` are unchanged; lookups found one more fragment (E6). The two timing results are the code before and after Phase 9, run back to back on the same machine (E4).

Compare any two with `sourced bench compare A.json B.json`.

## Caveats

- **Two code versions.** The first six runs used the Phase 6 harness with uncommitted Phase 7 changes; `clean-small` ran on the next commit. Metric definitions didn't change between them, but rerun before quoting small differences.
- **Search metrics** (`ranking.search`: is the right article found without a URL?) are only in the reference result.
- **Timing:** `timing` runs on the baseline settings, `timing-clean-small` on the chosen ones. Timed results vary between runs and machines; compare only runs made back to back.
- **Tokens** use OpenAI's `cl100k_base` encoding: a relative measure for Claude, not an exact one.
- **Ranking** has 25 questions, plus 25 held-out ones since Phase 9: enough to see large differences, not small ones (one question is 4 points). Rankers are chosen on the first set and checked on the held-out set.
