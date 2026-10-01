package bench

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"log/slog"
	"time"

	"github.com/sourcednet/core"
	"github.com/sourcednet/publisher"
	"github.com/sourcednet/resolver"
)

// CorrectionsConfig configures E5.
type CorrectionsConfig struct {
	// Samples is how many corrections to publish in each mode.
	Samples int `yaml:"samples" json:"samples"`
	// PollIntervalMs is how often the resolver polls the publisher's manifest.
	PollIntervalMs int `yaml:"poll_interval_ms" json:"poll_interval_ms"`
}

// correctionsMetrics answers E5: how long after a publisher corrects a page
// does a resolver serve the correction? Real time, in milliseconds.
type correctionsMetrics struct {
	Samples      int   `json:"samples"`
	PollInterval int   `json:"poll_interval_ms"`
	PollingOnly  stats `json:"polling_only_ms"`
	WithAnnounce stats `json:"with_announce_ms"`
	// Resolve answers are always checked live, so they show a correction
	// as soon as it is published, whatever the resolver has synced.
	Note string `json:"note"`
}

func corrections(ctx context.Context, p *pages, cfg CorrectionsConfig, pt parts, log *slog.Logger) (*correctionsMetrics, error) {
	poll := time.Duration(cfg.PollIntervalMs) * time.Millisecond
	site, err := newReplaySite(p, pt, func(c *resolver.Config) {
		c.Freshness, c.PollInterval, c.MinPoll, c.AnnounceEvery = time.Hour, poll, poll, time.Millisecond
	})
	if err != nil {
		return nil, err
	}
	defer site.close()
	arts := p.c.Articles[:min(cfg.Samples, len(p.c.Articles))]
	for _, a := range arts {
		if err := site.put(p, a, len(a.Revisions)-1); err != nil {
			return nil, err
		}
	}
	if _, err := site.build(time.Now(), nil); err != nil {
		return nil, err
	}
	r := site.resolver
	if err := r.Sync(ctx, BenchDomain); err != nil {
		return nil, err
	}
	runCtx, stop := context.WithCancel(ctx)
	defer stop()
	go r.Run(runCtx, 10*time.Millisecond)

	m := &correctionsMetrics{Samples: len(arts), PollInterval: cfg.PollIntervalMs,
		Note: "resolve answers are checked live, so they show a correction immediately"}
	var polling, announced []float64
	for round, announce := range []bool{false, true} {
		for i, a := range arts {
			d, err := timeCorrection(ctx, site, p, a, round*len(arts)+i, announce, 3*poll+5*time.Second)
			if err != nil {
				return nil, err
			}
			if announce {
				announced = append(announced, d)
			} else {
				polling = append(polling, d)
			}
		}
	}
	log.Info("corrections", "samples", len(arts))
	m.PollingOnly, m.WithAnnounce = summarize(polling), summarize(announced)
	return m, nil
}

// timeCorrection publishes a correction to one article and measures how
// long until the resolver points the page at the new record.
func timeCorrection(ctx context.Context, site *replaySite, p *pages, a Article, n int, announce bool, timeout time.Duration) (float64, error) {
	doc, err := p.doc(a, len(a.Revisions)-1)
	if err != nil {
		return 0, err
	}
	notice := fmt.Sprintf("<h1>%s</h1>\n<p>Correction %d: an earlier version of this article contained an error that has now been fixed by the editors.</p>", html.EscapeString(a.Title), n)
	doc = bytes.Replace(doc, []byte("<h1>"+html.EscapeString(a.Title)+"</h1>"), []byte(notice), 1)
	if err := site.write(a, doc); err != nil {
		return 0, err
	}
	url := pageURL(a)
	old, _, _, err := site.resolver.Store().Current(ctx, url)
	if err != nil {
		return 0, err
	}
	decl := map[string]publisher.Declaration{url: {Change: core.ChangeCorrection, Note: fmt.Sprintf("Benchmark correction %d.", n)}}
	if _, err := site.build(time.Now(), decl); err != nil {
		return 0, err
	}
	start := time.Now()
	if announce {
		if err := site.resolver.Announce(BenchDomain); err != nil {
			return 0, err
		}
	}
	for time.Since(start) < timeout {
		cur, _, _, err := site.resolver.Store().Current(ctx, url)
		if err != nil {
			return 0, err
		}
		if cur != old {
			return float64(time.Since(start).Microseconds()) / 1000, nil
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(5 * time.Millisecond):
		}
	}
	return 0, fmt.Errorf("%s: correction not seen within %s", a.Title, timeout)
}
