# E2 · Token efficiency

**Question:** how many tokens does an AI app spend on a page: raw HTML, reader-mode text, all signed chunks, or only the query's best chunks?

**Setup:** `wikipedia-100`, first version of each article, `cl100k_base` encoding (a relative measure for Claude). Readability is go-readability, a typical reader mode. The query is the article's title (a simplification; the real questions are in the ranking experiment), and the app gets the top 3 chunks.

## Numbers

Per page, averaged over 100 articles:

| What the app reads | Tokens | vs reader mode |
| --- | --- | --- |
| Raw HTML | 204,400 | 7.9× |
| Reader mode (readability) | 25,900 | 1× |
| All signed chunks, baseline extraction | 93,700 | 3.6× |
| All signed chunks, clean extraction | 31,900 | 1.2× |
| 3 best chunks, baseline (300/1500/4000) | 663 | 2.6% |
| 3 best chunks, small (150/800/2000) | 404 | 1.6% |
| 3 best chunks, large (600/3000/6000) | 1,234 | 4.8% |
| **3 best chunks, clean-small** | **487** | **1.9%** |

Metrics: `tokens.raw_html`, `readability_text`, `sourced_all_chunks`, `per_page_query_chunks_tokens`.

### Phase 9: link-list sections left out

Since the first E7 session, clean extraction also leaves out the See also, External links, Further reading, References, and Bibliography sections (`extraction.drop_sections`). Result `20261001T091459Z-clean-small` against `20261001T071929Z-clean-small`:

- Tokens per page: 31,900 → **25,900**, now 1.0× a reader-mode extraction (was 1.2×).
- Three passages for the title query: 488 → **396** tokens, 65× fewer than reader mode (was 53×).

## Conclusion

- **Query-ranked passages are the saving:** about 490 tokens instead of about 26,000 for reader mode (53× fewer) or about 204,000 for raw HTML (420× fewer). This works only if the right passages rank first: see `ranking-search.md`.
- **The whole signed page costs about the same as reader mode** when extraction is clean (1.2×). It's slightly more because infoboxes and tables stay: they hold facts. Without clean extraction it's 3.6×, mostly reference lists.
- **Chunk size scales passage cost directly:** small chunks cost a third of large ones for the same 3 passages.
- **Spec:** "Model context" in the efficiency table holds. The 3-passage figure is in the chunk-size rule.
