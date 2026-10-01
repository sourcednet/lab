package bench

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"

	"github.com/pkoukk/tiktoken-go"
	"github.com/sourcednet/core"
	"github.com/sourcednet/resolver"
	"github.com/sourcednet/resolver/rank"
	"go.yaml.in/yaml/v3"
)

// RankingConfig configures the ranking experiment.
type RankingConfig struct {
	Questions string `yaml:"questions" json:"questions"`
	// HeldOut is a second question set, kept out of choosing rankers, to
	// check that a ranker's gains hold on questions it wasn't tuned on.
	HeldOut string `yaml:"held_out" json:"held_out,omitempty"`
}

// Question is a question about one article and text its answer contains.
type Question struct {
	Article  string `yaml:"article" json:"article"`
	Question string `yaml:"question" json:"question"`
	Answer   string `yaml:"answer" json:"answer"`
}

// LoadQuestions reads a question set.
func LoadQuestions(path string) ([]Question, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f struct {
		Questions []Question `yaml:"questions"`
	}
	if err := yaml.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return f.Questions, nil
}

// rankingMetrics answers: does query ranking put the passage that answers
// a question near the top? It ranks each question against its article's
// chunks exactly as resolvers and the MCP tools do, with the configured
// ranker, on the question set and on the held-out set.
type rankingMetrics struct {
	Ranker string `json:"ranker"`
	questionMetrics
	HeldOut *questionMetrics `json:"held_out,omitempty"`
}

// questionMetrics scores one question set.
type questionMetrics struct {
	Questions int `json:"questions"`
	// HitAtK is the share of questions with an answering passage in the top k.
	Hit1  float64 `json:"hit_at_1"`
	Hit3  float64 `json:"hit_at_3"`
	Hit5  float64 `json:"hit_at_5"`
	Hit10 float64 `json:"hit_at_10"`
	// MRR is the mean of 1/rank of the first answering passage (0 if none).
	MRR float64 `json:"mrr"`
	// Answerable is the share whose answer appears in any chunk at all;
	// questions that aren't can't be ranked well, whatever the ranking.
	Answerable float64       `json:"answerable"`
	Paging     pagingMetrics `json:"paging"`
	// Search is the same questions asked across all articles at once, as
	// sourced_search does when the app has no URL.
	Search      searchMetrics  `json:"search"`
	PerQuestion []questionRank `json:"per_question"`
}

// pagingMetrics follows a model that reads a page's passages pageSize at a
// time with sourced_fetch, asking for the next ones (offset) until one
// holds the answer, and gives up after maxPages calls.
type pagingMetrics struct {
	PageSize int `json:"page_size"`
	MaxPages int `json:"max_pages"`
	// Answered is the share of questions answered within MaxPages calls.
	Answered float64 `json:"answered"`
	// Pages and Tokens are what each question cost on average, answered
	// or not: calls made, and tokens of passage text read.
	Pages  float64 `json:"mean_pages"`
	Tokens float64 `json:"mean_tokens"`
	// TokensPerPage is the mean passage tokens in one call's answer.
	TokensPerPage float64 `json:"tokens_per_page"`
}

// The paging model: the MCP tools' default of 5 passages per call, and up
// to 3 calls.
const (
	pageSize = 5
	maxPages = 3
)

// searchMetrics scores search across all articles: is the right article
// among the top pages, and does a returned passage hold the answer?
type searchMetrics struct {
	PageHit1 float64 `json:"page_hit_at_1"`
	PageHit3 float64 `json:"page_hit_at_3"`
	PageHit5 float64 `json:"page_hit_at_5"`
	// AnswerInTop5 is the share of questions where one of the best passages
	// of the top 5 pages contains the answer.
	AnswerInTop5 float64 `json:"answer_in_top5_passages"`
	// AnswerReturned is the same over every passage a search returns: the
	// best of each of the top 5 pages, and more of the first page
	// (resolver.TopResultPassages).
	AnswerReturned float64 `json:"answer_in_returned_passages"`
}

type questionRank struct {
	Question string `json:"question"`
	// Rank is the 1-based rank of the first answering passage; 0 if none.
	Rank int `json:"rank"`
}

func ranking(ctx context.Context, p *pages, cfg RankingConfig, rk rank.Ranker, tc TokensConfig, log *slog.Logger) (*rankingMetrics, error) {
	enc, err := encoding(tc.Encoding)
	if err != nil {
		return nil, err
	}
	m := &rankingMetrics{Ranker: rk.Name()}
	qs, err := LoadQuestions(cfg.Questions)
	if err != nil {
		return nil, err
	}
	if m.questionMetrics, err = rankQuestions(ctx, p, qs, rk, enc); err != nil {
		return nil, err
	}
	if cfg.HeldOut != "" {
		held, err := LoadQuestions(cfg.HeldOut)
		if err != nil {
			return nil, err
		}
		qm, err := rankQuestions(ctx, p, held, rk, enc)
		if err != nil {
			return nil, err
		}
		m.HeldOut = &qm
	}
	log.Info("ranking", "ranker", m.Ranker, "questions", m.Questions)
	return m, nil
}

// passages returns a chunk list as ranking sees it.
func passagesOf(cs []core.Chunk) []rank.Passage {
	ps := make([]rank.Passage, len(cs))
	for i, c := range cs {
		ps[i] = rank.Passage{Section: c.Section, Text: c.Text}
	}
	return ps
}

// answers reports whether a passage holds a question's answer, in the text
// a reader sees.
func answers(p rank.Passage, q Question) bool {
	return strings.Contains(strings.ToLower(rank.Document(p.Section, p.Text)), strings.ToLower(q.Answer))
}

func rankQuestions(ctx context.Context, p *pages, qs []Question, rk rank.Ranker, enc *tiktoken.Tiktoken) (questionMetrics, error) {
	byTitle := map[string]Article{}
	for _, a := range p.c.Articles {
		byTitle[a.Title] = a
	}
	var m questionMetrics
	var hit1, hit3, hit5, hit10, answerable, answered, pages, pageTokens int
	var rr float64
	for _, q := range qs {
		if err := ctx.Err(); err != nil {
			return m, err
		}
		a, ok := byTitle[q.Article]
		if !ok {
			continue // not in this (possibly limited) corpus
		}
		_, cs := p.chunks(a, len(a.Revisions)-1)
		ps := passagesOf(cs)
		if slices.ContainsFunc(ps, func(x rank.Passage) bool { return answers(x, q) }) {
			answerable++
		}
		order := rank.Order(rk, q.Question, ps)
		r := 0
		for pos, i := range order {
			if answers(ps[i], q) {
				r = pos + 1
				break
			}
		}
		m.Questions++
		m.PerQuestion = append(m.PerQuestion, questionRank{Question: q.Question, Rank: r})
		if r > 0 {
			rr += 1 / float64(r)
			hit1 += b2i(r == 1)
			hit3 += b2i(r <= 3)
			hit5 += b2i(r <= 5)
			hit10 += b2i(r <= 10)
		}
		// Paging: read pageSize passages per call until the answer shows up.
		for offset, n := 0, 0; n < maxPages; n++ {
			page, more := rank.Slice(order, offset, pageSize)
			for _, i := range page {
				pageTokens += len(enc.EncodeOrdinary(cs[i].Text + "\n\n"))
			}
			pages++
			if r > 0 && r <= offset+len(page) {
				answered++
				break
			}
			if !more {
				break
			}
			offset += len(page)
		}
	}
	var err error
	if m.Search, err = searchAcross(p, qs, byTitle, rk); err != nil {
		return m, err
	}
	m.Hit1, m.Hit3, m.Hit5, m.Hit10 = ratio(hit1, m.Questions), ratio(hit3, m.Questions), ratio(hit5, m.Questions), ratio(hit10, m.Questions)
	m.Answerable = ratio(answerable, m.Questions)
	m.Paging = pagingMetrics{PageSize: pageSize, MaxPages: maxPages, Answered: ratio(answered, m.Questions)}
	if m.Questions > 0 {
		m.MRR = round(rr / float64(m.Questions))
		m.Paging.Pages = round(float64(pages) / float64(m.Questions))
		m.Paging.Tokens = round(float64(pageTokens) / float64(m.Questions))
	}
	if pages > 0 {
		m.Paging.TokensPerPage = round(float64(pageTokens) / float64(pages))
	}
	return m, nil
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// searchAcross asks each question across every article's latest revision,
// ranking passages and grouping them by article like a resolver's search.
func searchAcross(p *pages, qs []Question, byTitle map[string]Article, rk rank.Ranker) (searchMetrics, error) {
	var ps []rank.Passage
	var group []int
	var titles []string
	for gi, a := range p.c.Articles {
		_, cs := p.chunks(a, len(a.Revisions)-1)
		ps = append(ps, passagesOf(cs)...)
		for range cs {
			group = append(group, gi)
		}
		titles = append(titles, a.Title)
	}
	var m searchMetrics
	asked, hit1, hit3, hit5, inTop, returned := 0, 0, 0, 0, 0, 0
	for _, q := range qs {
		if _, ok := byTitle[q.Article]; !ok {
			continue
		}
		asked++
		hits, _ := rank.Slice(rank.Groups(rk, q.Question, ps, group), 0, 5)
		answered, answeredMore := false, false
		for pos, h := range hits {
			if titles[h.Group] == q.Article {
				hit1 += b2i(pos == 0)
				hit3 += b2i(pos < 3)
				hit5++
			}
			if answers(ps[h.Passages[0]], q) {
				answered = true
			}
			if pos == 0 {
				more, _ := rank.Slice(h.Passages, 1, resolver.TopResultPassages-1)
				for _, i := range more {
					answeredMore = answeredMore || answers(ps[i], q)
				}
			}
		}
		inTop += b2i(answered)
		returned += b2i(answered || answeredMore)
	}
	m.PageHit1, m.PageHit3, m.PageHit5, m.AnswerInTop5 = ratio(hit1, asked), ratio(hit3, asked), ratio(hit5, asked), ratio(inTop, asked)
	m.AnswerReturned = ratio(returned, asked)
	return m, nil
}
