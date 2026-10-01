package bench

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/sourcednet/resolver"
	"github.com/sourcednet/resolver/verifier"
)

// LatencyConfig configures E4.
type LatencyConfig struct {
	// Samples is how many articles to fetch.
	Samples int `yaml:"samples" json:"samples"`
	// PublisherDelayMs simulates the network round trip to the publisher,
	// added to every response. The fake network has none of its own.
	PublisherDelayMs int `yaml:"publisher_delay_ms" json:"publisher_delay_ms"`
}

// latencyMetrics answers E4: how long does a verified fetch take, cold and
// warm, directly and through a resolver? Times are in milliseconds and vary
// between runs.
type latencyMetrics struct {
	Samples        int   `json:"samples"`
	PublisherDelay int   `json:"publisher_delay_ms"`
	DirectCold     stats `json:"direct_cold_ms"`
	DirectWarm     stats `json:"direct_warm_ms"`
	ResolverCold   stats `json:"resolver_cold_ms"`
	// ResolverWarm is fetched right after the cold fetch, while the
	// resolver is still indexing the page in the background.
	ResolverWarm stats `json:"resolver_warm_ms"`
	// ResolverIndexed is fetched once that indexing has finished.
	ResolverIndexed stats `json:"resolver_warm_indexed_ms"`
}

func latency(ctx context.Context, p *pages, cfg LatencyConfig, pt parts, log *slog.Logger) (*latencyMetrics, error) {
	site, err := newReplaySite(p, pt, func(c *resolver.Config) { c.Freshness = time.Hour })
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
	site.faults.Delay(time.Duration(cfg.PublisherDelayMs) * time.Millisecond)

	client := &resolver.Client{Base: "https://resolver.test", HTTP: site.net.Client()}
	var dc, dw, rc, rw, ri []float64
	timed := func(dst *[]float64, fetch func() (*verifier.Page, error)) error {
		start := time.Now()
		page, err := fetch()
		if err != nil {
			return err
		}
		if page.Verification != verifier.Verified {
			return fmt.Errorf("%s: %s %s", page.URL, page.Verification, page.Reason)
		}
		*dst = append(*dst, float64(time.Since(start).Microseconds())/1000)
		return nil
	}
	for _, a := range arts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		u := pageURL(a)
		direct := verifier.New(site.net.Client())
		resolverFetch := func() (*verifier.Page, error) { return client.Fetch(ctx, u) }
		for _, step := range []struct {
			dst   *[]float64
			fetch func() (*verifier.Page, error)
		}{
			{&dc, func() (*verifier.Page, error) { return direct.Fetch(ctx, u) }}, // new verifier: nothing cached
			{&dw, func() (*verifier.Page, error) { return direct.Fetch(ctx, u) }}, // keys cached; record and bundle fetched again
			{&rc, resolverFetch}, // the resolver has never seen the page
			{&rw, resolverFetch}, // within the freshness window: answered from the store
		} {
			if err := timed(step.dst, step.fetch); err != nil {
				return nil, err
			}
		}
		if _, err := site.resolver.Store().IndexPending(ctx); err != nil { // wait for the background indexer
			return nil, err
		}
		if err := timed(&ri, resolverFetch); err != nil {
			return nil, err
		}
	}
	log.Info("latency", "samples", len(arts))
	return &latencyMetrics{
		Samples: len(arts), PublisherDelay: cfg.PublisherDelayMs,
		DirectCold: summarize(dc), DirectWarm: summarize(dw), ResolverCold: summarize(rc), ResolverWarm: summarize(rw), ResolverIndexed: summarize(ri),
	}, nil
}
