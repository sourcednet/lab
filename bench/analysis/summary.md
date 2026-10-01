# Summary of results

Measured on 100 Wikipedia articles and 21 consecutive revisions of each (1,997 real edits), with the chosen settings: clean extraction and 150/800/2,000-character chunks. The details are in the notes beside this file.

| | Result |
| --- | --- |
| **Tokens per answer** | 3 verified passages cost about **400 tokens**, against about 26,000 for a reader-mode copy of the page (**65× fewer**) and 204,000 for its raw HTML (520× fewer). |
| **Citations that last** | **93%** of citations (median per article) still point to the exact same passage after 20 real edits; per edit, 99%. When one does change, `resolve` says so and offers the passage's latest version. |
| **Finding sources without a URL** | The right article is the top search result for **96%** of plain questions, and in the top 3 for all. The passages search returns hold the answer for **80%** (76% on a held-out set). |
| **Finding the answer in a page** | The passage that answers a plain question is among the first 5 a page returns for **80%** of questions (88% on a held-out set), and among the first 10, one more call, for **88%** (88%). The rest ask in words the answer doesn't use ("tall" vs. elevation), which needs semantic ranking. |
| **Finding a quote's source** | Exact quotes, fragments, and slightly edited quotes are traced to their signed page in **all 900** sampled cases, with no ambiguous matches. |
| **Corrections reach answers** | About **0.1 s** from publishing to the resolver serving the correction when the publisher announces; at the next poll otherwise. Checking a citation (`resolve`) always asks the publisher live. |
| **Speed** | **14 ms** for a resolver that holds the page, and **194 ms** for one that has to fetch and verify it first, about the same as verifying directly against the publisher (165–218 ms). With a 50 ms simulated round trip. |
| **Cost per 1,000 pages** | Publisher: about **190 MB** of static files per version of every page. Resolver: about **410 MB** (3.8 GB before Phase 9), of which 104 MB is the signed text and most of the rest the passage index that finds a quote's source. Each further version adds about 40 KB. These are long pages (125,000 characters of signed text on average), and costs scale with text length. |
| **Citation accuracy** | Pending: E7, scored by hand in Claude Code. |

**The main lesson:** what a publisher signs matters more than how it's chunked. Leaving out reference lists, footnote markers, and navigation took citation survival from 63% to 92% and cut tokens and storage by about 3×. The spec now says so.

**Not measured yet:** real networks, other kinds of sites (news, documentation, blogs), and answer quality as a user sees it (E7).
