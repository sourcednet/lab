package bench

import (
	"context"
	"log/slog"
	"unicode/utf8"

	"github.com/sourcednet/core"
)

// stabilityMetrics answers E1: do chunk IDs (citations) survive real edits?
type stabilityMetrics struct {
	Transitions int `json:"transitions"`
	// UnchangedShare is the share of edits that didn't change the signed
	// Markdown at all (e.g. edits to templates the extraction drops).
	UnchangedShare float64 `json:"unchanged_share"`
	// Survival is, per edit that changed the page, the share of the old
	// version's chunk IDs still present in the new version.
	Survival stats `json:"survival_per_edit"`
	// SurvivalOverall weights every chunk equally across all edits.
	SurvivalOverall float64 `json:"survival_overall"`
	// NewChunks is how many chunk IDs each changing edit introduces.
	NewChunks stats `json:"new_chunks_per_edit"`
	// FirstToLast is, per article, the share of the first revision's chunk
	// IDs still present in the last.
	FirstToLast stats `json:"survival_first_to_last"`
	// Pages and chunk sizes, from each article's latest revision.
	ChunksPerPage stats `json:"chunks_per_page"`
	ChunkChars    stats `json:"chunk_chars"`
}

func idSet(cs []core.Chunk) map[string]bool {
	m := make(map[string]bool, len(cs))
	for _, c := range cs {
		m[c.ID()] = true
	}
	return m
}

func stability(ctx context.Context, p *pages, log *slog.Logger) (*stabilityMetrics, error) {
	m := &stabilityMetrics{}
	var survival, newChunks, firstLast, perPage, chars []float64
	unchanged, kept, total := 0, 0, 0
	for ai, a := range p.c.Articles {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var first, prev map[string]bool
		var prevMD string
		var last []core.Chunk
		for i := range a.Revisions {
			md, cs := p.chunks(a, i)
			if cs == nil {
				prev = nil
				continue
			}
			ids := idSet(cs)
			if first == nil {
				first = ids
			}
			if prev != nil {
				m.Transitions++
				if md == prevMD {
					unchanged++
				} else {
					k := 0
					for id := range prev {
						if ids[id] {
							k++
						}
					}
					kept += k
					total += len(prev)
					survival = append(survival, float64(k)/float64(len(prev)))
					n := 0
					for id := range ids {
						if !prev[id] {
							n++
						}
					}
					newChunks = append(newChunks, float64(n))
				}
			}
			prev, prevMD, last = ids, md, cs
		}
		if first != nil && last != nil {
			lastIDs, k := idSet(last), 0
			for id := range first {
				if lastIDs[id] {
					k++
				}
			}
			firstLast = append(firstLast, float64(k)/float64(len(first)))
			perPage = append(perPage, float64(len(last)))
			for _, c := range last {
				chars = append(chars, float64(utf8.RuneCountInString(c.Text)))
			}
		}
		if (ai+1)%10 == 0 {
			log.Info("stability", "articles", ai+1, "of", len(p.c.Articles))
		}
	}
	m.UnchangedShare = ratio(unchanged, m.Transitions)
	m.Survival = summarize(survival)
	m.SurvivalOverall = ratio(kept, total)
	m.NewChunks = summarize(newChunks)
	m.FirstToLast = summarize(firstLast)
	m.ChunksPerPage = summarize(perPage)
	m.ChunkChars = summarize(chars)
	return m, nil
}
