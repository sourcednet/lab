# E7 · Citation quality in Claude Code

E7 is the one experiment done by hand: does an AI app with the sourced tools answer from the right passage, quote it faithfully, cite it precisely, and surface corrections? It has two parts:

- **Part A** uses the 25 questions in `../questions/wikipedia-100.yaml`, whose answers are known to be in each article. Claude already knows most of these answers, so this part tests citing, not knowing: the tools can only add tokens here.
- **Part B** asks where provenance is the point: content Claude can't know (the sample sites), quotes to attribute, an altered quote, and a site that doesn't sign.

## Setup (once)

```
make build
bin/sourced-lab demo prepare -dir "$HOME/sourced-e7" -corpus bench/corpus/wikipedia-100
claude mcp add --scope user sourced-e7 -- "$PWD/bin/sourced-lab" demo -dir "$HOME/sourced-e7"
```

`demo prepare` does the slow work once, before anything connects (about 30 seconds): it publishes the corpus's 100 articles (latest revisions, with clean extraction) as `wikipedia.test`, and has the resolver sync and index them. Serving the prepared folder then starts instantly. Follow along with `tail -f ~/sourced-e7/sourced.log`.

**Since the split into projects (Oct 1, 2026),** the demo is `sourced-lab demo`, built in `Dev/lab`. If you registered `sourced-e7` with the old `bin/sourced` from `Dev/sourcednet`, register it again with the command above; the prepared folder keeps working. Run the `demo prepare` line once, then reconnect (`/mcp` in Claude Code).

**After Phase 9:** clean extraction also leaves out the See also, External links, Further reading, References, and Bibliography sections. `demo prepare` keeps sites that already exist, so to sign the corpus the new way, delete `~/sourced-e7/wikipedia.test` and run `demo prepare` again.

## Procedure

### Part A: Wikipedia questions

For each question, in a **new conversation** (so earlier answers don't help):

1. Ask it in plain words, without a URL: *"Using the sourced tools: <question>"*. Claude should find the page with `sourced_search`, read it with `sourced_fetch`, and answer with a quote and a cite link.
2. Score the answer in `scores.csv` (copy `scores-template.csv`).

Then, for three of the questions (Apollo 11, Penicillin, Cape Verde), test corrections:

3. Change the answer in the page: edit `~/sourced-e7/wikipedia.test/public/wiki/<slug>.html` (for example, change the date; the slug is the title in lowercase with dashes, such as `apollo-11`), then publish it as a correction:
   ```
   ../publisher/bin/sourced-publisher build -page wiki/<slug>.html -correction "Corrected <what> for E7." ~/sourced-e7/wikipedia.test
   ```
4. In the same conversation, ask: *"Does the source you cited still stand?"* Score whether Claude called `sourced_resolve` and reported the correction, its note, and the new value.

### Part B: where provenance matters

Same procedure, one new conversation per question. The rows are at the end of `scores-template.csv`.

| Question | Expected |
| --- | --- |
| How much did the repairs of the old stone bridge in the river valley cost? | 2.4 million, from daily-herald.test (then run the correction steps above on `news/2026/09/bridge-reopens.html` in `~/sourced-e7/daily-herald.test`, changing it to 3.1 million) |
| Are heavy trucks allowed on the river valley's old stone bridge? | No: trucks over 7.5 tonnes remain banned |
| When is this year's harvest festival in the river valley? | The second weekend of October |
| Which checksum algorithms does Tidemark support? | sha256 or blake3 (devdocs.test) |
| How does the Example Library name files when digitizing a book? | Collection, item number, zero-padded page number, such as `lib-0042-p0001.tiff` |
| Who wrote "the archive grew in ways its founders had not planned for"? | The essay "On keeping things" on longform.test, found with `sourced_verify` |
| Did the Daily Herald write "The repairs cost 3 million, well over budget"? | No: only a partial match; the signed text says 2.4 million, slightly under budget |
| What does plain-site.test say about itself? | Answered, but not presented as signed: the site doesn't take part |

**Optional comparison:** ask the same questions with Claude's normal web fetch against the live `https://en.wikipedia.org/wiki/<Title>`, and score them the same way. That makes requests to Wikipedia from your account's tools; it's your call.

## Scoring

One row per question; 1 for yes, 0 for no.

| Column | Yes when |
| --- | --- |
| `found_page` | Claude found the right article (with `sourced_search` or otherwise) |
| `used_tool` | Claude answered from `sourced_fetch` or `sourced_search` passages (not its normal web fetch) |
| `correct` | The answer is right (contains the expected answer) |
| `quoted` | Claude quoted the passage it relied on |
| `quote_exact` | The quote is word for word in the passage (check with `sourced_verify`, or `sourced-resolver inspect search`) |
| `cited` | Claude gave a `cite` link |
| `cite_right` | The cited chunk contains the answer (`sourced-resolver inspect -data ~/sourced-e7 chunk <id>`) |
| `signed_wording` | Claude said "signed by wikipedia.test" (or similar), not "verified true" |
| `correction_seen` | (correction rows only) Claude reported the correction and its note |
| `tool_calls` | How many sourced tool calls Claude made (a number), for the token side |

Note anything odd in `notes`: wrong page, a hallucinated quote, the ranking missing the answer (compare with the `ranking` experiment's per-question ranks), and so on.

## First try (Oct 1, before Phase 9's fixes)

An informal session on Part A questions, with Claude's own assessment:

- **Attribution worked:** every claim came with an exact quote, a cite link, the publisher, and its state.
- **Tokens:** no saving on these questions. Claude knew the answers, and the tools added cost.
- **Search often needed a second call:** it found the right page, but its one passage didn't hold the answer (Linux, Apollo 11, photosynthesis, World War II). Phase 9 gives the first result 3 passages.
- **Noise:** a Linux image caption, Apollo 11's "See also" list, and Tidemark's install page for "Linux". Phase 9 leaves link-list sections out of what is signed. Captions stay, since some hold facts (the Bastille's date). Other sites' pages still match vague queries; `publisher` narrows search.
- **Tool results carried the full signed answer** as structured content, a second copy of every passage, which Claude read too. Since Phase 9 they carry only the text written for the model.

## What to look for

- Whether misses come from finding the page (search), from ranking within it (the answer wasn't in the passages returned), or from the model. The `ranking` experiment's numbers are the automatic counterpart: search finds the right article first for 96% of these questions.
- Whether citations point to the passage that holds the answer, not just to the page.
- Whether the "signed by, not true" distinction survives in Claude's wording.
- In Part B, whether the tools pay for their tokens: answers Claude couldn't give otherwise, quotes traced or refuted, unsigned content kept apart.
