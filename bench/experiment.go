package bench

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/sourcednet/core"
	"github.com/sourcednet/publisher"
	"github.com/sourcednet/resolver"
	"github.com/sourcednet/resolver/passage"
	"github.com/sourcednet/resolver/rank"
	"go.yaml.in/yaml/v3"
)

// Config is one experiment run.
type Config struct {
	Name   string `yaml:"name" json:"name"`
	Corpus string `yaml:"corpus" json:"corpus"`
	// Articles limits the run to the first N articles (0 = all), for quick runs.
	Articles   int              `yaml:"articles" json:"articles"`
	Chunking   core.ChunkParams `yaml:"chunking" json:"chunking"`
	Extraction ExtractionConfig `yaml:"extraction" json:"extraction"`
	// Ranker orders passages for queries, by name (see rank.Named).
	Ranker string `yaml:"ranker" json:"ranker"`
	// Index is the resolver's passage index, by name (see passage.Named).
	Index       string            `yaml:"index" json:"index"`
	Experiments []string          `yaml:"experiments" json:"experiments"`
	Tokens      TokensConfig      `yaml:"tokens" json:"tokens"`
	Passages    PassagesConfig    `yaml:"passages" json:"passages"`
	Latency     LatencyConfig     `yaml:"latency" json:"latency"`
	Corrections CorrectionsConfig `yaml:"corrections" json:"corrections"`
	Ranking     RankingConfig     `yaml:"ranking" json:"ranking"`
}

// ExtractionConfig says what of each page is signed content.
type ExtractionConfig struct {
	// Drop lists elements to leave out, as publisher drop selectors.
	// Empty signs everything in the content, like a naive publisher.
	Drop []string `yaml:"drop" json:"drop"`
	// DropSections lists section headings to leave out with their sections,
	// as the publisher's drop_sections.
	DropSections []string `yaml:"drop_sections" json:"drop_sections,omitempty"`
}

func (x ExtractionConfig) publisher() publisher.Extraction {
	return publisher.Extraction{Drop: x.Drop, DropSections: x.DropSections}
}

// WikipediaDrop leaves out what isn't article prose on Wikipedia pages:
// reference lists and markers, navigation boxes, hatnotes, images, and
// maintenance notices. Infoboxes stay, captions included: they hold facts
// (the Bastille's date is only in its infobox caption).
var WikipediaDrop = []string{
	".reflist", "ol.references", ".mw-references-wrap", "sup.reference", ".navbox", ".navbox-styles",
	".hatnote", "figure", ".thumb", ".metadata", ".sistersitebox", ".noprint", ".mw-empty-elt",
}

// WikipediaDropSections leaves out the sections of Wikipedia pages that
// list links and sources rather than say anything.
var WikipediaDropSections = []string{"See also", "External links", "Further reading", "References", "Bibliography"}

// TokensConfig configures E2.
type TokensConfig struct {
	Encoding    string `yaml:"encoding" json:"encoding"`
	QueryChunks int    `yaml:"query_chunks" json:"query_chunks"`
}

// PassagesConfig configures E6.
type PassagesConfig struct {
	PerArticle int    `yaml:"per_article" json:"per_article"`
	Seed       uint64 `yaml:"seed" json:"seed"`
}

// The experiments a config can name.
var knownExperiments = []string{"stability", "tokens", "cost", "passages", "ranking", "latency", "corrections"}

// timedExperiments measure wall-clock time, so their metrics vary between
// runs; every other experiment is deterministic.
var timedExperiments = []string{"latency", "corrections"}

// LoadConfig reads an experiment file and fills in defaults.
func LoadConfig(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	c := DefaultConfig()
	dec := yaml.NewDecoder(strings.NewReader(string(b)))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if c.Name == "" || c.Corpus == "" {
		return nil, fmt.Errorf("%s: want a name and a corpus", path)
	}
	if err := c.Chunking.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	for _, d := range c.Extraction.Drop {
		if _, err := publisher.ParseSelector(d); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
	}
	if _, err := c.parts(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	for _, e := range c.Experiments {
		if !slices.Contains(knownExperiments, e) {
			return nil, fmt.Errorf("%s: unknown experiment %q (known: %s)", path, e, strings.Join(knownExperiments, ", "))
		}
	}
	return &c, nil
}

// parts are the resolver implementations a run uses: the bench judges
// them against each other with the same corpus and questions.
type parts struct {
	ranker rank.Ranker
	index  passage.Index
}

func (c *Config) parts() (parts, error) {
	rk, err := rank.Named(c.Ranker)
	if err != nil {
		return parts{}, err
	}
	idx, err := passage.Named(c.Index)
	return parts{ranker: rk, index: idx}, err
}

// configure sets a resolver to use the run's implementations.
func (pt parts) configure(c *resolver.Config) { c.Ranker, c.Index = pt.ranker, pt.index }

// Result is what a run writes. Metrics are deterministic (the same config
// and corpus give the same metrics) except for the experiments listed in
// Timed, which measure wall-clock time. Timings are kept apart.
type Result struct {
	Name     string             `json:"name"`
	Config   Config             `json:"config"`
	Corpus   CorpusInfo         `json:"corpus"`
	Git      GitInfo            `json:"git"`
	Go       string             `json:"go"`
	Started  time.Time          `json:"started"`
	Metrics  map[string]any     `json:"metrics"`
	Timed    []string           `json:"timed_experiments,omitempty"`
	Timings  map[string]float64 `json:"timings_seconds"`
	Warnings []string           `json:"warnings,omitempty"`
}

// CorpusInfo identifies the corpus a result was computed on.
type CorpusInfo struct {
	Name     string `json:"name"`
	Checksum string `json:"checksum"`
	Articles int    `json:"articles"`
}

// GitInfo identifies the code a result was computed with.
type GitInfo struct {
	Commit string `json:"commit"`
	Dirty  bool   `json:"dirty"`
}

// Run runs the configured experiments.
func Run(ctx context.Context, cfg *Config, log *slog.Logger) (*Result, error) {
	c, err := LoadCorpus(cfg.Corpus)
	if err != nil {
		return nil, err
	}
	if cfg.Articles > 0 && cfg.Articles < len(c.Articles) {
		c.Articles = c.Articles[:cfg.Articles]
	}
	res := &Result{
		Name:    cfg.Name,
		Config:  *cfg,
		Corpus:  CorpusInfo{Name: c.Name, Checksum: c.Checksum, Articles: len(c.Articles)},
		Git:     gitInfo(),
		Go:      runtime.Version(),
		Started: time.Now().UTC().Truncate(time.Second),
		Metrics: map[string]any{},
		Timings: map[string]float64{},
	}
	pages := newPages(c, cfg.Chunking, cfg.Extraction.publisher())
	pt, err := cfg.parts()
	if err != nil {
		return nil, err
	}
	for _, e := range cfg.Experiments {
		start := time.Now()
		log.Info("experiment", "name", e, "articles", len(c.Articles))
		var m any
		var err error
		switch e {
		case "stability":
			m, err = stability(ctx, pages, log)
		case "tokens":
			m, err = tokens(ctx, pages, cfg.Tokens, pt.ranker, log)
		case "cost":
			m, err = cost(ctx, pages, pt, log)
		case "passages":
			m, err = passages(ctx, pages, cfg.Passages, pt.index, log)
		case "ranking":
			m, err = ranking(ctx, pages, cfg.Ranking, pt.ranker, cfg.Tokens, log)
		case "latency":
			m, err = latency(ctx, pages, cfg.Latency, pt, log)
		case "corrections":
			m, err = corrections(ctx, pages, cfg.Corrections, pt, log)
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e, err)
		}
		res.Metrics[e] = m
		if slices.Contains(timedExperiments, e) {
			res.Timed = append(res.Timed, e)
		}
		res.Timings[e] = time.Since(start).Round(time.Millisecond).Seconds()
		log.Info("experiment done", "name", e, "took", time.Since(start).Round(time.Millisecond))
	}
	res.Warnings = pages.warnings
	return res, nil
}

// Write stores a result in dir as <started>-<name>.json and returns its path.
func (r *Result) Write(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, r.Started.Format("20060102T150405Z")+"-"+r.Name+".json")
	return path, os.WriteFile(path, append(b, '\n'), 0o644)
}

func gitInfo() GitInfo {
	var g GitInfo
	if out, err := exec.Command("git", "rev-parse", "HEAD").Output(); err == nil {
		g.Commit = strings.TrimSpace(string(out))
	}
	if out, err := exec.Command("git", "status", "--porcelain", "--untracked-files=no").Output(); err == nil {
		g.Dirty = len(strings.TrimSpace(string(out))) > 0
	}
	return g
}

// DefaultConfig returns a config with every default filled in.
func DefaultConfig() Config {
	return Config{
		Chunking:    core.DefaultChunkParams,
		Ranker:      rank.Default.Name(),
		Index:       passage.Default.Name(),
		Experiments: knownExperiments,
		Tokens:      TokensConfig{Encoding: "cl100k_base", QueryChunks: 3},
		Passages:    PassagesConfig{PerArticle: 3, Seed: 1},
		Latency:     LatencyConfig{Samples: 20, PublisherDelayMs: 50},
		Corrections: CorrectionsConfig{Samples: 10, PollIntervalMs: 2000},
		Ranking:     RankingConfig{Questions: "bench/questions/wikipedia-100.yaml", HeldOut: "bench/questions/wikipedia-100-heldout.yaml"},
	}
}
