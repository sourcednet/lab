package bench

import (
	"fmt"
	"html"
	"sort"

	"github.com/sourcednet/core"
	"github.com/sourcednet/publisher"
)

// BenchDomain is the domain corpus articles are published on.
const BenchDomain = "wikipedia.test"

// pages turns corpus revisions into what a publisher would sign: an HTML
// page, its Markdown, and its chunks, using the publisher's own extraction.
type pages struct {
	c          *Corpus
	params     core.ChunkParams
	extraction publisher.Extraction
	warnings   []string
}

func newPages(c *Corpus, params core.ChunkParams, x publisher.Extraction) *pages {
	return &pages{c: c, params: params, extraction: x}
}

// PagePath is an article's path in a web root.
func PagePath(a Article) string { return "wiki/" + a.Slug + ".html" }

func pageURL(a Article) string { return "https://" + BenchDomain + "/" + PagePath(a) }

// Page returns one revision of an article as a full HTML page, as a
// publisher would serve it.
func (c *Corpus) Page(a Article, rev int) ([]byte, error) {
	body, err := c.HTML(a.Revisions[rev])
	if err != nil {
		return nil, err
	}
	t := html.EscapeString(a.Title)
	return []byte(fmt.Sprintf("<!doctype html>\n<html lang=\"en\">\n<head>\n<meta charset=\"utf-8\">\n<title>%s</title>\n</head>\n<body>\n<main>\n<h1>%s</h1>\n%s\n</main>\n</body>\n</html>\n", t, t, body)), nil
}

func (p *pages) doc(a Article, i int) ([]byte, error) { return p.c.Page(a, i) }

// chunks returns a revision's Markdown and chunks. A revision that can't be
// processed is skipped with a warning (nil chunks).
func (p *pages) chunks(a Article, i int) (string, []core.Chunk) {
	doc, err := p.doc(a, i)
	if err == nil {
		var md string
		_, _, md, err = publisher.ExtractMarkdown(doc, p.extraction, pageURL(a))
		if err == nil {
			var cs []core.Chunk
			cs, err = core.ChunkMarkdown(md, p.params)
			if err == nil {
				return md, cs
			}
		}
	}
	p.warnings = append(p.warnings, fmt.Sprintf("%s revision %d: %v", a.Title, a.Revisions[i].ID, err))
	return "", nil
}

// stats summarizes a sample of numbers.
type stats struct {
	N      int     `json:"n"`
	Mean   float64 `json:"mean"`
	Median float64 `json:"median"`
	P10    float64 `json:"p10"`
	P90    float64 `json:"p90"`
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
}

func summarize(xs []float64) stats {
	if len(xs) == 0 {
		return stats{}
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	sum := 0.0
	for _, x := range s {
		sum += x
	}
	q := func(f float64) float64 { return round(s[int(f*float64(len(s)-1))]) }
	return stats{N: len(s), Mean: round(sum / float64(len(s))), Median: q(0.5), P10: q(0.1), P90: q(0.9), Min: round(s[0]), Max: round(s[len(s)-1])}
}

// round keeps four decimals, so results read cleanly and compare exactly.
func round(x float64) float64 {
	const k = 10000
	if x < 0 {
		return -float64(int64(-x*k+0.5)) / k
	}
	return float64(int64(x*k+0.5)) / k
}

func ratio(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return round(float64(a) / float64(b))
}
