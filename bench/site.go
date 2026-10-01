package bench

import (
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/sourcednet/publisher"
	"github.com/sourcednet/resolver"
	"github.com/sourcednet/testkit/fakenet"
)

// replaySite is a publisher on the fake network, holding corpus articles as
// pages, with a resolver that can reach it. Experiments that need the real
// publish and sync path (cost, latency, corrections) build on it.
type replaySite struct {
	net      *fakenet.Network
	faults   *fakenet.Faults
	pub      *publisher.Config
	resolver *resolver.Resolver
	// store is the resolver's store, kept for measuring it (cost).
	store *resolver.Store
	tmp   string
}

// newReplaySite starts an empty site, with a resolver using the run's
// parts. tune adjusts the resolver's config before it starts (clock,
// freshness, polling).
func newReplaySite(p *pages, pt parts, tune func(*resolver.Config)) (*replaySite, error) {
	tmp, err := os.MkdirTemp("", "sourced-bench-site-")
	if err != nil {
		return nil, err
	}
	s := &replaySite{tmp: tmp}
	if s.net, err = fakenet.Start(); err != nil {
		s.close()
		return nil, err
	}
	if s.pub, err = publisher.Init(filepath.Join(tmp, "publisher"), BenchDomain, "public", "", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		s.close()
		return nil, err
	}
	s.pub.Chunking, s.pub.DropSelectors, s.pub.DropSections = p.params, p.extraction.Drop, p.extraction.DropSections
	s.faults = s.net.AddRoot(BenchDomain, s.pub.RootDir())
	dataDir := filepath.Join(tmp, "resolver")
	if s.store, err = resolver.OpenStore(dataDir, pt.index); err != nil {
		s.close()
		return nil, err
	}
	cfg := resolver.Config{
		Name: "resolver.test", DataDir: dataDir, HTTP: s.net.Client(), Store: s.store,
		Logger: slog.New(slog.DiscardHandler),
	}
	pt.configure(&cfg)
	if tune != nil {
		tune(&cfg)
	}
	if s.resolver, err = resolver.New(cfg); err != nil {
		s.store.Close()
		s.close()
		return nil, err
	}
	s.net.AddHandler(cfg.Name, s.resolver.Handler())
	return s, nil
}

// put writes one revision of an article as its page in the web root.
func (s *replaySite) put(p *pages, a Article, rev int) error {
	doc, err := p.doc(a, rev)
	if err != nil {
		return err
	}
	return s.write(a, doc)
}

// write replaces an article's page with doc.
func (s *replaySite) write(a Article, doc []byte) error {
	path := filepath.Join(s.pub.RootDir(), filepath.FromSlash(PagePath(a)))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, doc, 0o644)
}

// build signs what changed, as of now.
func (s *replaySite) build(now time.Time, declare map[string]publisher.Declaration) (*publisher.Report, error) {
	return publisher.Build(s.pub, publisher.BuildOptions{Now: now, Declare: declare})
}

func (s *replaySite) close() {
	if s.resolver != nil {
		s.resolver.Close()
	}
	if s.net != nil {
		s.net.Close()
	}
	os.RemoveAll(s.tmp)
}
