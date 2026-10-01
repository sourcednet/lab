# Ranking and search

**Question:** with a plain question, does the resolver find the right page (search), and put the answering passage near the top of it (ranking)?

**Setup:** 25 questions with known answers (`bench/questions/wikipedia-100.yaml`), plus 25 held-out questions from Phase 9, ranked with BM25 over chunk text (links and URLs left out, stopwords, light stemming). Ranking scores chunks within the right page. Search ranks pages across all 100 articles, each by its best passage, and is in the `clean-small` reference result only. 25 questions show large differences, not small ones (one question is 4 points).

## Numbers

Ranking within the page:

| Config | hit@1 | hit@3 | hit@5 | MRR |
| --- | --- | --- | --- | --- |
| baseline | 48% | 68% | 76% | 0.60 |
| small-chunks | 40% | 76% | **88%** | 0.60 |
| medium-chunks | 40% | 72% | 80% | 0.58 |
| large-chunks | **56%** | 72% | 76% | **0.66** |
| clean-extraction | 44% | 72% | 76% | 0.60 |
| clean-small | 44% | 72% | 80% | 0.60 |

Search across pages (`clean-small`): the right article is first for **96%** of questions and in the top 3 for **100%**. The page's best passage holds the exact answer for 48%.

Metrics: `ranking.hit_at_*`, `mrr`, `search.*`, with ranks per question in `ranking.per_question`.

### Phase 9: a second ranker, a held-out set, and paging

Results `20261001T071929Z-clean-small` (`bm25`, the default) and `20261001T071307Z-clean-small-lead` (`bm25-lead`: BM25, with passages in a page's opening, before its first subheading, counting double). Both use the extraction fix that keeps table cells apart.

The **held-out set** (`bench/questions/wikipedia-100-heldout.yaml`) is 25 new questions on other articles, written before any ranker was tried on them; answers found in more than about a dozen chunks were replaced, since any ranking hits them. Rankers were chosen on the first set only.

| | bm25: questions | bm25: held out | bm25-lead: questions | bm25-lead: held out |
| --- | --- | --- | --- | --- |
| hit@1 | 44% | 52% | **64%** | 52% |
| hit@3 | 72% | 84% | 80% | 88% |
| hit@5 | 80% | 88% | 84% | 88% |
| hit@10 | 88% | 88% | 92% | 88% |
| MRR | 0.60 | 0.68 | **0.74** | 0.69 |
| Search: right page first | 96% | 96% | 96% | **88%** |
| Search: answer in a returned passage | 48% | 44% | 72% | 48% |

**Paging**, as the MCP tools now allow: the model reads 5 passages per `sourced_fetch` call and asks for the next 5 until it has the answer, for up to 3 calls (`ranking.paging.*`, `bm25`):

| | Questions | Held out |
| --- | --- | --- |
| Answered in the first call (hit@5) | 80% | 88% |
| Answered within 3 calls (hit@15) | 88% | 88% |
| Calls per question, on average | 1.32 | 1.24 |
| Tokens per call (5 passages) | 929 | 990 |
| Tokens per question, on average | 1,226 | 1,228 |

Questions never answered count as 3 calls. Tokens are passage text only (`cl100k_base`), as in E2.

**After the first E7 session** (`20261001T091459Z-clean-small`, `20261001T092239Z-clean-small-lead`): link-list sections are left out of what is signed (ranks barely move: Ada Lovelace 2 → 3, deep misses up a few places), and search gives its first result three passages instead of one. The answer is now among the passages a search returns for **80%** of questions (76% held out), up from 52% (48%) with one passage per page (`search.answer_in_returned_passages`). Paging costs about 900 tokens per call. MCP results also stopped carrying a second, structured copy of every answer, which had about doubled what the model read per call.

## Conclusion

- **Finding the page works at this scale.** Search is what makes the demo usable without URLs.
- **Ranking passages within a page is the weak spot.** The misses (`clean-small`) are:
  - numeric facts held in infobox tables: Everest's height (rank 77), Lake Baikal's depth (45);
  - a date phrased differently from the question: the Apollo 11 landing (38);
  - "who created / painted" questions whose answer uses other words (Python 6, Mona Lisa 7, Git 5).

  Keyword ranking can't bridge that wording gap.
- **Large chunks rank best at the top** (more context per chunk) but cost 2.5× the tokens (E2). Small chunks catch more in the top 5.
- **The tools are built to compensate:** the model searches, then fetches the page with its question and gets 5 passages, where hit@5 applies. E7 will show how well that works in practice.
- **Phase 9: facts in tables.** Infoboxes holding images or lists can't become Markdown tables, and their cells were signed run together ("Elevation8,848.86 m"). The publisher now keeps cells apart. The facts read correctly, but ranks didn't move: Everest's height is still at 81, because "tall" and "elevation" share no word.
- **Phase 9: a second ranker.** Of five variants tried on the first question set, a prior for a page's opening helped most (best-sentence scoring, alone or mixed with BM25, did not). On the held-out set the gain mostly disappears (MRR 0.68 → 0.69), and search put the right page first less often (96% → 88%). It can also favor an opening paragraph that only mentions the query's words, as in the resolver's news-page test. So `bm25` stays the default, and `bm25-lead` is selectable (`ranker:` in configs, `-ranker` on `serve`, `mcp`, and `demo`).
- **Phase 9: paging.** It reaches the near misses (Python at 6, Mona Lisa at 7: 80% → 88%) for about one more call, roughly 930 tokens. It can't reach the deep misses, at ranks 17 to 84.
- **What's left is mostly wording.** Of the six questions missed within 15 passages, on both sets, five ask in words the answer doesn't use: "tall" vs. elevation, "deep" vs. depth, "designed" for TCP, "how large" vs. area, "originally" for coffee. The sixth, Apollo 11's landing date, matches only "landing", once, in a long infobox. Keyword ranking can't close that gap. Semantic ranking (embeddings or a re-ranker) is the next step, and the bench can now judge it on both sets. It needs a model, which means a new dependency and a download, so it waits for a decision. Embeddings stay a resolver choice (Future Versions).
