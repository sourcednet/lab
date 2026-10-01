package bench

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"log/slog"
	"math/rand/v2"
	"os"
	"strings"

	"github.com/sourcednet/core"
	"github.com/sourcednet/resolver"
	"github.com/sourcednet/resolver/passage"
)

// passagesMetrics answers E6: does the passage index find the source of a
// quote? Samples come from each article's latest revision; the index holds
// every version of every article, as a resolver's would.
type passagesMetrics struct {
	Exact   lookupScore `json:"exact_sentence"`
	Trimmed lookupScore `json:"fragment"`
	Edited  lookupScore `json:"edited_sentence"`
}

// lookupScore summarizes lookups of one kind of quote.
type lookupScore struct {
	Samples int `json:"samples"`
	// Found: the right article is among the matches.
	Found float64 `json:"found"`
	// Top1: the right article is in the best match.
	Top1 float64 `json:"top1"`
	// ExactMatch: the best match is word for word.
	ExactMatch float64 `json:"exact_match"`
	// Overlap: mean share of the quote's windows in the best match.
	Overlap float64 `json:"mean_overlap"`
	// Ambiguous: the best match appears in more than one article.
	Ambiguous float64 `json:"ambiguous"`
}

type quote struct {
	kind, text, url string
}

func passages(ctx context.Context, p *pages, cfg PassagesConfig, idx passage.Index, log *slog.Logger) (*passagesMetrics, error) {
	dir, err := os.MkdirTemp("", "sourced-bench-passages-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	store, err := resolver.OpenStore(dir, idx)
	if err != nil {
		return nil, err
	}
	defer store.Close()

	seed := sha256.Sum256([]byte("bench signing key"))
	priv := ed25519.NewKeyFromSeed(seed[:])
	rng := rand.New(rand.NewPCG(cfg.Seed, 0))
	var quotes []quote
	for ai, a := range p.c.Articles {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var latest []core.Chunk
		for i, rev := range a.Revisions {
			_, cs := p.chunks(a, i)
			if cs == nil {
				continue
			}
			r := core.NewRecord(core.PageMeta{Publisher: BenchDomain, URL: pageURL(a), Title: a.Title, PublishedAt: rev.Timestamp}, cs)
			if err := core.SignRecord(r, "bench", priv); err != nil {
				return nil, err
			}
			raw, _ := json.Marshal(r)
			if err := store.PutRecord(ctx, raw, r); err != nil {
				return nil, err
			}
			b, err := core.NewBundle(r, cs)
			if err != nil {
				return nil, err
			}
			if err := store.PutBundle(ctx, r, b); err != nil {
				return nil, err
			}
			latest = cs
		}
		quotes = append(quotes, sampleQuotes(rng, latest, pageURL(a), cfg.PerArticle)...)
		if (ai+1)%10 == 0 {
			log.Info("passages: indexed", "articles", ai+1, "of", len(p.c.Articles))
		}
	}

	byKind := map[string][]quote{}
	for _, q := range quotes {
		byKind[q.kind] = append(byKind[q.kind], q)
	}
	m := &passagesMetrics{}
	for kind, dst := range map[string]*lookupScore{"exact": &m.Exact, "trimmed": &m.Trimmed, "edited": &m.Edited} {
		s, err := score(ctx, store, byKind[kind])
		if err != nil {
			return nil, err
		}
		*dst = s
	}
	return m, nil
}

// sampleQuotes picks up to n prose chunks and makes three quotes from each:
// one whole sentence, a fragment from inside it, and the sentence with one
// word changed.
func sampleQuotes(rng *rand.Rand, cs []core.Chunk, url string, n int) []quote {
	var prose []core.Chunk
	for _, c := range cs {
		if !strings.HasPrefix(c.Text, "|") && !strings.HasPrefix(c.Text, "- ") && !strings.HasPrefix(c.Text, "```") {
			prose = append(prose, c)
		}
	}
	rng.Shuffle(len(prose), func(i, j int) { prose[i], prose[j] = prose[j], prose[i] })
	var out []quote
	for _, c := range prose {
		if len(out) >= 3*n {
			break
		}
		var long []string
		for _, s := range core.SplitSentences(strings.Join(strings.Fields(c.Text), " ")) {
			if len(strings.Fields(s)) >= 12 {
				long = append(long, s)
			}
		}
		if len(long) == 0 {
			continue
		}
		sentence := long[rng.IntN(len(long))]
		words := strings.Fields(sentence)
		// A fragment of 8 to 12 words from inside the sentence: never its
		// first or last word, so it is never a whole sentence.
		size := min(8+rng.IntN(5), len(words)-2)
		start := 1 + rng.IntN(len(words)-size-1)
		edited := append([]string(nil), words...)
		edited[len(words)/2] = "notably"
		out = append(out,
			quote{"exact", sentence, url},
			quote{"trimmed", strings.Join(words[start:start+size], " "), url},
			quote{"edited", strings.Join(edited, " "), url},
		)
	}
	return out
}

func score(ctx context.Context, store *resolver.Store, qs []quote) (lookupScore, error) {
	s := lookupScore{Samples: len(qs)}
	if len(qs) == 0 {
		return s, nil
	}
	var found, top1, exact, ambiguous int
	var overlap float64
	for _, q := range qs {
		hits, err := store.LookupPassage(ctx, q.text, 10)
		if err != nil {
			return s, err
		}
		if len(hits) == 0 {
			continue
		}
		in := func(h resolver.PassageHit) bool {
			for _, o := range h.Occurrences {
				if o.URL == q.url {
					return true
				}
			}
			return false
		}
		for _, h := range hits {
			if in(h) {
				found++
				break
			}
		}
		if in(hits[0]) {
			top1++
		}
		if hits[0].Match == "exact" {
			exact++
		}
		urls := map[string]bool{}
		for _, o := range hits[0].Occurrences {
			urls[o.URL] = true
		}
		if len(urls) > 1 {
			ambiguous++
		}
		overlap += hits[0].Overlap
	}
	s.Found, s.Top1, s.ExactMatch, s.Ambiguous = ratio(found, len(qs)), ratio(top1, len(qs)), ratio(exact, len(qs)), ratio(ambiguous, len(qs))
	s.Overlap = round(overlap / float64(len(qs)))
	return s, nil
}
