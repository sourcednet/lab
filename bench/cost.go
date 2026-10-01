package bench

import (
	"context"
	"io/fs"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/sourcednet/publisher"
	"github.com/sourcednet/resolver"
)

// costMetrics answers E3: what does it cost a publisher and a resolver to
// keep up with a site? Every revision is replayed in order through a real
// publisher build and resolver sync, on the fake network.
type costMetrics struct {
	Pages    int        `json:"pages"`
	Versions int        `json:"versions"`
	Steps    []costStep `json:"steps"`
	// Final totals, after all versions.
	ResolverBytes      int64   `json:"resolver_bytes"`
	PublisherBytes     int64   `json:"publisher_bytes"`
	ResolverPerPage    int64   `json:"resolver_bytes_per_page"`
	ResolverPerVersion int64   `json:"resolver_bytes_per_version"`
	DedupRatio         float64 `json:"dedup_ratio"`
	WindowsPerChunk    float64 `json:"index_windows_per_chunk"`
	// What the resolver's database holds: the passage index and the
	// publishers' signed records.
	ResolverIndexBytes  int64 `json:"resolver_index_bytes"`
	ResolverRecordBytes int64 `json:"resolver_record_bytes"`
}

// costStep is the state after replaying one revision of every page.
type costStep struct {
	Step           int   `json:"step"`
	ChangedPages   int   `json:"changed_pages"`
	BundlesFetched int   `json:"bundles_fetched"`
	Records        int   `json:"records"`
	Chunks         int   `json:"chunks"`
	ChunkBytes     int64 `json:"chunk_bytes"`
	IndexWindows   int   `json:"index_windows"`
	DBBytes        int64 `json:"db_bytes"`
}

func cost(ctx context.Context, p *pages, pt parts, log *slog.Logger) (*costMetrics, error) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now := t0
	site, err := newReplaySite(p, pt, func(c *resolver.Config) {
		c.Freshness, c.Now = 0, func() time.Time { return now } // a fixed clock keeps metrics deterministic
	})
	if err != nil {
		return nil, err
	}
	defer site.close()
	r := site.resolver

	steps := 0
	for _, a := range p.c.Articles {
		steps = max(steps, len(a.Revisions))
	}
	m := &costMetrics{Pages: len(p.c.Articles)}
	const bundles = "GET /.well-known/sourced/bundles/"
	for k := 0; k < steps; k++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for _, a := range p.c.Articles {
			if k < len(a.Revisions) {
				if err := site.put(p, a, k); err != nil {
					return nil, err
				}
			}
		}
		now = t0.Add(time.Duration(k) * time.Hour)
		rep, err := site.build(now, nil)
		if err != nil {
			return nil, err
		}
		before := site.net.Requests(BenchDomain, bundles)
		if err := r.Sync(ctx, BenchDomain); err != nil {
			p.warnings = append(p.warnings, "cost: sync: "+err.Error())
		}
		if _, err := site.store.IndexPending(ctx); err != nil { // finish what the resolver indexes in the background
			return nil, err
		}
		if err := site.store.Checkpoint(ctx); err != nil {
			return nil, err
		}
		st, err := site.store.Stats(ctx)
		if err != nil {
			return nil, err
		}
		changed := len(rep.Pages) - rep.Count(publisher.ActionUnchanged)
		m.Steps = append(m.Steps, costStep{
			Step: k, ChangedPages: changed, BundlesFetched: site.net.Requests(BenchDomain, bundles) - before,
			Records: st.Records, Chunks: st.Chunks, ChunkBytes: st.ChunkBytes, IndexWindows: st.Windows, DBBytes: st.DBBytes,
		})
		log.Info("cost", "step", k+1, "of", steps, "changed_pages", changed, "records", st.Records, "chunks", st.Chunks)
		if k == steps-1 {
			m.Versions = st.Records
			m.ResolverBytes = st.DBBytes + st.ChunkBytes
			m.ResolverPerPage = m.ResolverBytes / int64(max(1, m.Pages))
			m.ResolverPerVersion = m.ResolverBytes / int64(max(1, st.Records))
			m.DedupRatio = ratio(st.ChunkRefs, st.Chunks)
			m.WindowsPerChunk = ratio(st.Windows, st.Chunks)
			m.ResolverIndexBytes, m.ResolverRecordBytes = st.IndexBytes, st.RecordBytes
		}
	}
	m.PublisherBytes = dirBytes(filepath.Join(site.pub.RootDir(), ".well-known"))
	return m, nil
}

func dirBytes(dir string) int64 {
	var n int64
	filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if fi, err := d.Info(); err == nil {
				n += fi.Size()
			}
		}
		return nil
	})
	return n
}
