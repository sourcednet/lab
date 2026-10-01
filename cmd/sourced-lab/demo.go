package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/sourcednet/resolver/cmdutil"
	"github.com/sourcednet/testkit/sites"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sourcednet/lab/bench"
	"github.com/sourcednet/publisher"
	"github.com/sourcednet/resolver"
	"github.com/sourcednet/resolver/httplog"
	"github.com/sourcednet/resolver/passage"
	"github.com/sourcednet/resolver/rank"
	"github.com/sourcednet/resolver/sourcedmcp"
	"github.com/sourcednet/resolver/verifier"
	"github.com/sourcednet/testkit/fakenet"
)

// demoStart is when the demo publishers first publish.
var demoStart = time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)

const demoResolver = "resolver.test"

// cmdDemo runs the sample publishers, a resolver, and the sourced MCP tools
// in one process, on fake domains over real HTTPS, so the whole flow can be
// tried in an AI app without deploying anything.
func cmdDemo(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "prepare" {
		return cmdDemoPrepare(ctx, args[1:], stdout, stderr)
	}
	fs := cmdutil.NewFlags("demo", "", stderr)
	dir := fs.String("dir", "sourced-demo", "a demo folder prepared by `sourced-lab demo prepare`")
	fs.String("sites", "", "ignored: preparing is `sourced-lab demo prepare`'s job (kept so older commands still work)")
	fs.String("corpus", "", "ignored: preparing is `sourced-lab demo prepare`'s job (kept so older commands still work)")
	local := fs.Bool("local", false, "use the validating client (checks everything locally) instead of the resolver")
	addr := fs.String("http", "", "serve MCP over HTTP on this address (e.g. 127.0.0.1:8090) instead of stdio")
	logs := cmdutil.AddLogFlags(fs, "log file (default <dir>/sourced.log); follow it with tail -f")
	rankerName, indexName := cmdutil.AddRankerFlag(fs), cmdutil.AddIndexFlag(fs)
	if code, ok := cmdutil.ParseFlags(fs, args); !ok {
		return code
	}
	ranker, err := rank.Named(*rankerName)
	if err != nil {
		return cmdutil.Fail(stderr, err)
	}
	index, err := passage.Named(*indexName)
	if err != nil {
		return cmdutil.Fail(stderr, err)
	}
	log, closeLog, err := logs.Open(stderr, filepath.Join(*dir, "sourced.log"))
	if err != nil {
		return cmdutil.Fail(stderr, err)
	}
	defer closeLog()
	log.Info("demo starting", "dir", *dir, "local", *local, "http", *addr, "ranker", ranker.Name(), "index", index.Name(), "version", version)

	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	opts := demoOptions{Dir: *dir, Local: *local, Ranker: ranker, Index: index}
	if *addr == "" {
		return runDemoStdio(ctx, opts, log, stderr)
	}
	return runDemoHTTP(ctx, opts, *addr, log, stderr)
}

// cmdDemoPrepare runs `sourced-lab demo prepare`: everything slow, once.
func cmdDemoPrepare(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := cmdutil.NewFlags("demo prepare", "", stderr)
	dir := fs.String("dir", "sourced-demo", "demo folder to prepare (created if missing; sites already there are kept)")
	sites := fs.String("sites", "", "sample sites to build the publishers from (default: testkit's sample sites)")
	corpus := fs.String("corpus", "", "also publish a sealed bench corpus (latest revisions) as "+bench.BenchDomain+", e.g. bench/corpus/wikipedia-100")
	logs := cmdutil.AddLogFlags(fs, "log file (default <dir>/sourced.log)")
	if code, ok := cmdutil.ParseFlags(fs, args); !ok {
		return code
	}
	log, closeLog, err := logs.Open(stderr, filepath.Join(*dir, "sourced.log"))
	if err != nil {
		return cmdutil.Fail(stderr, err)
	}
	defer closeLog()
	start := time.Now()
	if err := prepareDemoDir(ctx, prepareOptions{Dir: *dir, Sites: *sites, Corpus: *corpus}, log); err != nil {
		return cmdutil.Fail(stderr, err)
	}
	fmt.Fprintf(stdout, "Demo folder %s is ready (%s). Serve it with: sourced-lab demo -dir %s\n", *dir, time.Since(start).Round(time.Second), *dir)
	return 0
}

// runDemoStdio serves MCP over stdio at once and prepares the demo in the
// background: a first start (building sites, syncing the resolver) can take
// longer than an MCP client waits to connect.
func runDemoStdio(ctx context.Context, opts demoOptions, log *slog.Logger, stderr io.Writer) int {
	pending := newPendingBackend()
	var d *demo
	go func() {
		var err error
		d, err = startDemo(ctx, opts, log)
		if err != nil {
			log.Error("demo preparation failed", "err", err)
			pending.resolve(nil, err)
			return
		}
		log.Info("demo ready", "publishers", d.domains)
		pending.resolve(d.backend, nil)
	}()
	server := sourcedmcp.NewServer(pending, "sourced-demo", version, sourcedmcp.WithLogger(log))
	err := server.Run(ctx, &mcp.StdioTransport{})
	if _, perr := pending.wait(context.Background()); perr == nil {
		d.Close()
	}
	if err != nil && ctx.Err() == nil {
		return cmdutil.Fail(stderr, err)
	}
	return 0
}

// runDemoHTTP prepares the demo, then serves MCP and the resolver's API over
// HTTP. Started by hand, it can take its time.
func runDemoHTTP(ctx context.Context, opts demoOptions, addr string, log *slog.Logger, stderr io.Writer) int {
	d, err := startDemo(ctx, opts, log)
	if err != nil {
		return cmdutil.Fail(stderr, err)
	}
	defer d.Close()
	server := sourcedmcp.NewServer(d.backend, "sourced-demo", version, sourcedmcp.WithLogger(log))
	mux := http.NewServeMux()
	mux.Handle(cmdutil.MCPPath, mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil))
	if d.resolver != nil {
		mux.Handle("/", d.resolver.Handler())
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return cmdutil.Fail(stderr, err)
	}
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()
	fmt.Fprintf(stderr, "sourced-lab demo: publishers in %s: %v\n", d.dir, d.domains)
	fmt.Fprintf(stderr, "MCP over HTTP at http://%s%s\n", ln.Addr(), cmdutil.MCPPath)
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return cmdutil.Fail(stderr, err)
	}
	return 0
}

// demo is a running demo network.
type demo struct {
	dir      string
	domains  []string
	net      *fakenet.Network
	resolver *resolver.Resolver
	backend  sourcedmcp.Backend
}

func (d *demo) Close() {
	if d.resolver != nil {
		d.resolver.Close()
	}
	d.net.Close()
}

// startDemo prepares dir from sites on the first run, serves every site in
// dir on the fake network, and starts the backend.
// prepareOptions say what `sourced-lab demo prepare` puts in a demo folder.
type prepareOptions struct {
	Dir    string // demo folder: one publisher project per site
	Sites  string // sample sites to build the publishers from
	Corpus string // optional sealed bench corpus to publish as BenchDomain
}

// demoOptions say which prepared folder a demo serves and how it checks.
type demoOptions struct {
	Dir    string        // a folder prepared by `sourced-lab demo prepare`
	Local  bool          // validating client instead of a resolver
	Ranker rank.Ranker   // orders passages for queries and search
	Index  passage.Index // what the passage index stores
}

// prepareDemoDir does all of a demo's slow work, once, before anything
// connects: it builds the publishers and has the resolver sync and index
// them. Serving the folder afterwards only checks for changes.
func prepareDemoDir(ctx context.Context, opts prepareOptions, log *slog.Logger) error {
	if err := os.MkdirAll(opts.Dir, 0o755); err != nil {
		return err
	}
	if opts.Sites == "" {
		tmp, err := os.MkdirTemp("", "sourced-sample-sites-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmp)
		if err := sites.Write(tmp); err != nil {
			return err
		}
		opts.Sites = tmp
	}
	log.Info("preparing demo sites", "from", opts.Sites, "into", opts.Dir)
	if err := prepareDemo(opts.Dir, opts.Sites); err != nil {
		return err
	}
	if opts.Corpus != "" {
		if err := prepareCorpusSite(opts.Dir, opts.Corpus, log); err != nil {
			return err
		}
	}
	d, err := startDemo(ctx, demoOptions{Dir: opts.Dir}, log) // syncs every publisher
	if err != nil {
		return err
	}
	defer d.Close()
	start := time.Now()
	n, err := d.resolver.Store().IndexPending(ctx) // what the resolver would otherwise index in the background
	log.Info("index", "chunks", n, "took", time.Since(start).Round(time.Millisecond))
	return err
}

// startDemo serves a prepared demo folder: its sites on the fake network,
// and a resolver (or a local verifier). It never builds anything; its sync
// is a conditional request per publisher, quick when nothing changed.
func startDemo(ctx context.Context, opts demoOptions, log *slog.Logger) (*demo, error) {
	dir, local := opts.Dir, opts.Local
	n, err := fakenet.Start()
	if err != nil {
		return nil, err
	}
	d := &demo{dir: dir, net: n}
	entries, _ := os.ReadDir(dir) // a missing folder has no sites: reported below
	var publishers []string
	for _, e := range entries {
		root := filepath.Join(dir, e.Name(), "public")
		if !e.IsDir() || e.Name()[0] == '.' {
			continue
		}
		if _, err := os.Stat(root); err != nil {
			continue
		}
		n.AddRoot(e.Name(), root)
		d.domains = append(d.domains, e.Name())
		if _, err := os.Stat(filepath.Join(dir, e.Name(), publisher.ConfigFile)); err == nil {
			publishers = append(publishers, e.Name())
		}
	}
	if len(d.domains) == 0 {
		n.Close()
		return nil, fmt.Errorf("%s is not a prepared demo folder: run `sourced-lab demo prepare -dir %s [-corpus <dir>]` first", dir, dir)
	}

	client := httplog.Wrap(n.Client(), log)
	if local {
		d.backend = &sourcedmcp.Local{V: verifier.New(client, verifier.WithLogger(log)), Ranker: opts.Ranker}
		return d, nil
	}
	r, err := resolver.New(resolver.Config{
		Name: demoResolver, DataDir: filepath.Join(dir, ".resolver"), HTTP: client,
		Freshness: 0, // always recheck: edits in the demo directory show up at once
		Logger:    log, MCPPath: cmdutil.MCPPath, Ranker: opts.Ranker, Index: opts.Index,
	})
	if err != nil {
		n.Close()
		return nil, err
	}
	n.AddHandler(demoResolver, r.Handler())
	for _, p := range publishers {
		if err := r.Sync(ctx, p); err != nil {
			log.Warn("sync", "publisher", p, "err", err)
		}
	}
	d.resolver, d.backend = r, r
	return d, nil
}

// prepareSite creates one demo site, dir/name, unless it already exists. It
// builds into a hidden temporary folder and renames it into place when
// complete, so a start that is cut off never leaves a half-built site that
// a later start would take as ready.
func prepareSite(dir, name string, build func(tmp string) error) error {
	dst := filepath.Join(dir, name)
	if _, err := os.Stat(dst); err == nil {
		return nil
	}
	tmp := filepath.Join(dir, "."+name+".preparing")
	if err := os.RemoveAll(tmp); err != nil { // leftovers of an interrupted start
		return err
	}
	if err := build(tmp); err != nil {
		os.RemoveAll(tmp)
		return fmt.Errorf("%s: %w", name, err)
	}
	return os.Rename(tmp, dst)
}

// prepareDemo creates the sample sites that dir doesn't have yet: a
// publisher project per site, signed as of demoStart. Plain sites are copied.
func prepareDemo(dir, sites string) error {
	entries, err := os.ReadDir(sites)
	if err != nil {
		return fmt.Errorf("sample sites: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		src := filepath.Join(sites, e.Name())
		err := prepareSite(dir, e.Name(), func(tmp string) error {
			if _, err := os.Stat(filepath.Join(src, "PLAIN")); err == nil {
				return fakenet.CopyTree(filepath.Join(src, "public"), filepath.Join(tmp, "public"))
			}
			c, err := publisher.Init(tmp, e.Name(), "public", demoStart)
			if err != nil {
				return err
			}
			if err := fakenet.CopyTree(filepath.Join(src, "public"), c.RootDir()); err != nil {
				return err
			}
			_, err = publisher.Build(c, publisher.BuildOptions{Now: demoStart})
			return err
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// prepareCorpusSite publishes the latest revision of every article in a
// sealed corpus as a publisher project for bench.BenchDomain, once.
func prepareCorpusSite(dir, corpusDir string, log *slog.Logger) error {
	return prepareSite(dir, bench.BenchDomain, func(tmp string) error {
		c, err := bench.LoadCorpus(corpusDir)
		if err != nil {
			return err
		}
		log.Info("publishing corpus", "corpus", c.Name, "articles", len(c.Articles), "as", bench.BenchDomain)
		cfg, err := publisher.Init(tmp, bench.BenchDomain, "public", demoStart)
		if err != nil {
			return err
		}
		cfg.DropSelectors = bench.WikipediaDrop // sign article prose, not reference lists and navigation
		cfg.DropSections = bench.WikipediaDropSections
		if err := cfg.Save(tmp); err != nil {
			return err
		}
		for _, a := range c.Articles {
			page, err := c.Page(a, len(a.Revisions)-1)
			if err != nil {
				return err
			}
			path := filepath.Join(cfg.RootDir(), filepath.FromSlash(bench.PagePath(a)))
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(path, page, 0o644); err != nil {
				return err
			}
		}
		_, err = publisher.Build(cfg, publisher.BuildOptions{Now: demoStart})
		return err
	})
}
