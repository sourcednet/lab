package bench

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// paragraph is a deterministic paragraph of about 400 characters.
func paragraph(topic string, n, version int) string {
	var b strings.Builder
	for i := 0; b.Len() < 400; i++ {
		fmt.Fprintf(&b, "The %s section %d explains point %d in version %d of the article with plain words. ", topic, n, i, version)
	}
	return b.String()
}

// syntheticCorpus fetches a small corpus from a fake API: articles whose
// revision k rewrites one of five paragraphs.
func syntheticCorpus(t *testing.T, articles, revisions int) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch q.Get("action") {
		case "query":
			// Revision IDs are article*100 + n, so each article's text differs.
			var article int
			fmt.Sscanf(q.Get("titles"), "Article %d", &article)
			var revs []string
			for i := revisions; i >= 1; i-- {
				revs = append(revs, fmt.Sprintf(`{"revid":%d,"timestamp":"2026-09-%02dT00:00:00Z","size":1000}`, article*100+i, i))
			}
			fmt.Fprintf(w, `{"query":{"pages":[{"title":%q,"revisions":[%s]}]}}`, q.Get("titles"), strings.Join(revs, ","))
		case "parse":
			id, _ := strconv.Atoi(q.Get("oldid"))
			article, rev := id/100, id%100
			var body strings.Builder
			for p := 0; p < 5; p++ {
				v := 0
				if p < rev%5 || rev >= 5 {
					v = rev // revisions edit paragraphs one by one
				}
				fmt.Fprintf(&body, "<h2>Part %d</h2><p>%s</p>", p, paragraph(fmt.Sprintf("article %d", article), p, v))
			}
			b, _ := json.Marshal(map[string]any{"parse": map[string]any{"title": "T", "text": body.String()}})
			w.Write(b)
		}
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	var titles []string
	for i := 0; i < articles; i++ {
		titles = append(titles, fmt.Sprintf("Article %d", i))
	}
	dir := t.TempDir()
	_, err := (&Fetcher{HTTP: &http.Client{Transport: rewrite{u}}, Delay: time.Microsecond}).Fetch(context.Background(),
		&CorpusDef{Name: "synthetic", Lang: "en", Revisions: revisions, Titles: titles}, dir)
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestRunIsDeterministic(t *testing.T) {
	dir := syntheticCorpus(t, 3, 4)
	cfg := &Config{
		Name: "test", Corpus: dir, Chunking: DefaultConfig().Chunking,
		Experiments: []string{"stability", "cost", "passages"},
		Passages:    PassagesConfig{PerArticle: 2, Seed: 7},
	}
	var outs [2][]byte
	for i := range outs {
		res, err := Run(context.Background(), cfg, slog.New(slog.DiscardHandler))
		if err != nil {
			t.Fatal(err)
		}
		outs[i], _ = json.Marshal(res.Metrics)
		if i == 0 {
			st := res.Metrics["stability"].(*stabilityMetrics)
			if st.Transitions != 9 || st.Survival.N == 0 || st.SurvivalOverall <= 0 || st.SurvivalOverall >= 1 {
				t.Fatalf("stability: %+v", st)
			}
			c := res.Metrics["cost"].(*costMetrics)
			if len(c.Steps) != 4 || c.Steps[0].BundlesFetched != 3 || c.DedupRatio <= 1 {
				t.Fatalf("cost: %+v", c)
			}
			pm := res.Metrics["passages"].(*passagesMetrics)
			if pm.Exact.Samples == 0 || pm.Exact.Found != 1 || pm.Trimmed.Found != 1 {
				t.Fatalf("passages: %+v", pm)
			}
		}
	}
	if !bytes.Equal(outs[0], outs[1]) {
		t.Fatalf("metrics differ between identical runs:\n%s\n%s", outs[0], outs[1])
	}
}

func TestLoadConfigRejectsUnknownExperiment(t *testing.T) {
	path := t.TempDir() + "/x.yaml"
	writeFile(t, path, "name: x\ncorpus: c\nexperiments: [stability, magic]\n")
	if _, err := LoadConfig(path); err == nil || !strings.Contains(err.Error(), "magic") {
		t.Fatalf("got %v", err)
	}
}

func TestTimedExperiments(t *testing.T) {
	dir := syntheticCorpus(t, 3, 4)
	cfg := DefaultConfig()
	cfg.Name, cfg.Corpus = "timed", dir
	cfg.Experiments = []string{"latency", "corrections"}
	cfg.Latency = LatencyConfig{Samples: 2, PublisherDelayMs: 5}
	cfg.Corrections = CorrectionsConfig{Samples: 2, PollIntervalMs: 200}
	res, err := Run(context.Background(), &cfg, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	lat := res.Metrics["latency"].(*latencyMetrics)
	if lat.DirectCold.N != 2 || lat.DirectCold.Median < 5 || lat.ResolverWarm.Median >= lat.ResolverCold.Median {
		t.Fatalf("latency: %+v", lat)
	}
	cor := res.Metrics["corrections"].(*correctionsMetrics)
	if cor.PollingOnly.N != 2 || cor.WithAnnounce.Median >= cor.PollingOnly.Median {
		t.Fatalf("corrections: %+v", cor)
	}
	if len(res.Timed) != 2 {
		t.Fatalf("timed experiments not marked: %v", res.Timed)
	}
}

func TestShippedConfigsLoad(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("experiments", "*.yaml"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no configs found: %v", err)
	}
	for _, p := range paths {
		if _, err := LoadConfig(p); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}
	if qs, err := LoadQuestions(filepath.Join("questions", "wikipedia-100.yaml")); err != nil || len(qs) != 25 {
		t.Errorf("questions: %d, %v", len(qs), err)
	}
}
