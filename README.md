# lab

`sourced-lab` holds the benchmark (a frozen corpus of real revision histories, experiments run against it, results you can compare) and the demo, which runs sample publishers, a resolver, and the MCP tools in one process. It uses every other project; nothing here is deployed.

```
make            # lint, test, build: bin/sourced-lab
make bench      # every config in bench/experiments/ (minutes each; needs the corpus)
```

## Try it in Claude Code

The demo's publishers live on fake `.test` domains over real HTTPS inside one process, so nothing needs deploying.

```
make build
# Once: sign the sample sites and have the resolver sync and index them (a few seconds; ~30 s with -corpus).
bin/sourced-lab demo prepare -dir "$HOME/sourced-demo"
# Register the prepared folder with Claude Code (stdio). Use absolute paths.
claude mcp add sourced-demo -- "$PWD/bin/sourced-lab" demo -dir "$HOME/sourced-demo"
```

Then, in Claude Code:

1. **Ask:** "Using sourced, what did the repairs of the old stone bridge cost?" Claude should find the story with `sourced_search`, quote the passage, say it's signed by daily-herald.test, and give the cite link.
2. **Publish a correction**, in a terminal:
   ```
   sed -i 's/2.4 million/3.1 million/' ~/sourced-demo/daily-herald.test/public/news/2026/09/bridge-reopens.html
   ../publisher/bin/sourced-publisher build -page news/2026/09/bridge-reopens.html \
     -correction "An earlier version gave the wrong cost of the repairs." ~/sourced-demo/daily-herald.test
   ```
3. **Ask:** "Does the source you cited still stand?" Claude should call `sourced_resolve` and report the correction, its note, and the new figure.

Variations: `-http 127.0.0.1:8090` serves MCP over HTTP (`claude mcp add --transport http sourced-demo http://127.0.0.1:8090/mcp`); `-local` uses the validating client instead of the resolver; `-corpus bench/corpus/wikipedia-100` on `demo prepare` adds the Wikipedia corpus as `wikipedia.test` (see `bench/e7/README.md`). The demo logs to `<dir>/sourced.log` (`tail -f` it while you chat; the events are described in `../resolver/README.md`).

## Benchmarks

```
bin/sourced-lab bench fetch -corpus bench/corpora/wikipedia-100.yaml   # once: 100 articles x 21 revisions, polite and resumable
bin/sourced-lab bench run bench/experiments/clean-small.yaml           # writes bench/results/<time>-clean-small.json
bin/sourced-lab bench compare bench/results/A.json bench/results/B.json
```

| Experiment | Question |
| --- | --- |
| `stability` (E1) | Do chunk IDs (citations) survive real edits? |
| `tokens` (E2) | Tokens for raw HTML, readability text, all chunks, and query-ranked chunks |
| `cost` (E3) | Publisher and resolver storage, deduplication, and bundles fetched, replaying every revision |
| `latency` (E4) | Fetch time, cold and warm, direct and through a resolver (timed) |
| `corrections` (E5) | Time until a resolver serves a correction, with announce and polling only (timed) |
| `passages` (E6) | Does the passage index find the source of exact, trimmed, and edited quotes? |
| `ranking` | Does ranking put the answering passage in the top 1, 3, 5, or 10, and what does paging cost? On a question set and a held-out set |
| E7, by hand | Citation quality in Claude Code: see `bench/e7/README.md` |

Configs in `bench/experiments/` pick the chunk sizes, extraction (`extraction.drop`, `extraction.drop_sections`), and the resolver's parts (`ranker:`, `index:`); `clean-small` is the reference. Results are in `bench/results/`; `bench/analysis/README.md` maps them to experiments and holds the written analysis.

Metrics are deterministic (the same config and corpus give the same numbers) except for the timed experiments, which are marked in the result. Every result records the corpus checksum and git commit. Results before Oct 1, 2026 record commits of the monorepo these projects were split from (`../sourcednet`).

**The corpus** is fetched once from Wikipedia's API: one request at a time, with pauses, honoring Wikipedia's `maxlag` load signal, identifying itself with this project's URL, and storing no editor usernames. It is sealed with a checksum and an `ATTRIBUTION.md`. Wikipedia text is CC BY-SA 4.0, so the corpus stays out of git (`bench/corpus/`). Token counts use tiktoken's `cl100k_base` encoding, cached in `bench/.cache/`.
