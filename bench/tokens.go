package bench

import (
	"bytes"
	"context"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	readability "github.com/go-shiori/go-readability"
	"github.com/pkoukk/tiktoken-go"
	"github.com/sourcednet/resolver/rank"
)

// tokensMetrics answers E2: how much model context does each way of
// reading a page cost? Measured on each article's latest revision.
type tokensMetrics struct {
	Encoding string `json:"encoding"`
	Pages    int    `json:"pages"`
	// Totals over all pages, per representation.
	RawHTML     amount `json:"raw_html"`
	Readability amount `json:"readability_text"`
	Chunks      amount `json:"sourced_all_chunks"`
	QueryChunks amount `json:"sourced_query_chunks"`
	// Ratios of total tokens, against raw HTML and readability text.
	ChunksVsHTML        float64 `json:"chunks_vs_html"`
	ChunksVsReadability float64 `json:"chunks_vs_readability"`
	QueryVsHTML         float64 `json:"query_vs_html"`
	QueryVsReadability  float64 `json:"query_vs_readability"`
	// Per-page token counts, to see the spread.
	PerPageChunks      stats  `json:"per_page_chunks_tokens"`
	PerPageQueryChunks stats  `json:"per_page_query_chunks_tokens"`
	Query              string `json:"query"`
}

type amount struct {
	Tokens int `json:"tokens"`
	Chars  int `json:"chars"`
}

func (a *amount) add(enc *tiktoken.Tiktoken, s string) int {
	n := len(enc.EncodeOrdinary(s))
	a.Tokens += n
	a.Chars += utf8.RuneCountInString(s)
	return n
}

// encoding loads a tiktoken encoding, caching its file under bench/.cache
// so it is downloaded once.
func encoding(name string) (*tiktoken.Tiktoken, error) {
	if os.Getenv("TIKTOKEN_CACHE_DIR") == "" {
		dir := filepath.Join("bench", ".cache", "tiktoken")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
		os.Setenv("TIKTOKEN_CACHE_DIR", dir)
	}
	return tiktoken.GetEncoding(name)
}

func tokens(ctx context.Context, p *pages, cfg TokensConfig, rk rank.Ranker, log *slog.Logger) (*tokensMetrics, error) {
	enc, err := encoding(cfg.Encoding)
	if err != nil {
		return nil, err
	}
	m := &tokensMetrics{Encoding: cfg.Encoding, Query: "the article's title, top " + strconv.Itoa(cfg.QueryChunks) + " chunks"}
	var perPage, perQuery []float64
	for _, a := range p.c.Articles {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		last := len(a.Revisions) - 1
		doc, err := p.doc(a, last)
		if err != nil {
			return nil, err
		}
		_, cs := p.chunks(a, last)
		if cs == nil {
			continue
		}
		m.Pages++
		m.RawHTML.add(enc, string(doc))

		u, _ := url.Parse(pageURL(a))
		if art, err := readability.FromReader(bytes.NewReader(doc), u); err == nil {
			m.Readability.add(enc, art.TextContent)
		} else {
			p.warnings = append(p.warnings, a.Title+": readability: "+err.Error())
		}

		var all strings.Builder
		for _, c := range cs {
			all.WriteString(c.Text + "\n\n")
		}
		perPage = append(perPage, float64(m.Chunks.add(enc, all.String())))
		var top strings.Builder
		best, _ := rank.Slice(rank.Order(rk, a.Title, passagesOf(cs)), 0, cfg.QueryChunks)
		for _, i := range best {
			top.WriteString(cs[i].Text + "\n\n")
		}
		perQuery = append(perQuery, float64(m.QueryChunks.add(enc, top.String())))
	}
	log.Info("tokens", "pages", m.Pages)
	m.ChunksVsHTML = ratio(m.Chunks.Tokens, m.RawHTML.Tokens)
	m.ChunksVsReadability = ratio(m.Chunks.Tokens, m.Readability.Tokens)
	m.QueryVsHTML = ratio(m.QueryChunks.Tokens, m.RawHTML.Tokens)
	m.QueryVsReadability = ratio(m.QueryChunks.Tokens, m.Readability.Tokens)
	m.PerPageChunks = summarize(perPage)
	m.PerPageQueryChunks = summarize(perQuery)
	return m, nil
}
