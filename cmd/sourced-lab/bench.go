package main

import (
	"context"
	"fmt"
	"github.com/sourcednet/resolver/cmdutil"
	"io"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/sourcednet/lab/bench"
)

const benchUsage = `Usage: sourced-lab bench <command> [flags]

Commands:
  fetch -corpus <def.yaml> [-out dir]   fetch and seal a corpus (Wikipedia, polite, resumable)
  seal <corpus-dir>                     rewrite a corpus's ATTRIBUTION.md and checksum
  run <experiment.yaml>                 run an experiment, write a result to bench/results/
  compare <a.json> <b.json>             show two results side by side
`

func cmdBench(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, benchUsage)
		return 2
	}
	switch args[0] {
	case "fetch":
		return benchFetch(ctx, args[1:], stdout, stderr)
	case "seal":
		if len(args) != 2 {
			fmt.Fprint(stderr, benchUsage)
			return 2
		}
		c, err := bench.Reseal(args[1])
		if err != nil {
			return cmdutil.Fail(stderr, err)
		}
		fmt.Fprintf(stdout, "Corpus %s resealed: %d articles, checksum %s\n", c.Name, len(c.Articles), c.Checksum)
		return 0
	case "run":
		return benchRun(ctx, args[1:], stdout, stderr)
	case "compare":
		return benchCompare(ctx, args[1:], stdout, stderr)
	}
	fmt.Fprint(stderr, benchUsage)
	return 2
}

func benchFetch(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := cmdutil.NewFlags("bench fetch", "", stderr)
	defPath := fs.String("corpus", filepath.Join("bench", "corpora", "wikipedia-100.yaml"), "corpus definition")
	out := fs.String("out", "", "where to store the corpus (default bench/corpus/<name>)")
	delay := fs.Duration("delay", 500*time.Millisecond, "pause between requests")
	logs := cmdutil.AddLogFlags(fs, "also write progress to this file")
	if code, ok := cmdutil.ParseFlags(fs, args); !ok {
		return code
	}
	log, closeLog, err := logs.Open(stderr, "")
	if err != nil {
		return cmdutil.Fail(stderr, err)
	}
	defer closeLog()
	def, err := bench.LoadCorpusDef(*defPath)
	if err != nil {
		return cmdutil.Fail(stderr, err)
	}
	dir := *out
	if dir == "" {
		dir = filepath.Join("bench", "corpus", def.Name)
	}
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	log.Info("fetching corpus", "name", def.Name, "articles", len(def.Titles), "revisions", def.Revisions, "into", dir, "user_agent", bench.UserAgent)
	c, err := (&bench.Fetcher{Delay: *delay, Log: log}).Fetch(ctx, def, dir)
	if err != nil {
		return cmdutil.Fail(stderr, fmt.Errorf("%w (run again to resume)", err))
	}
	fmt.Fprintf(stdout, "Corpus %s sealed in %s: %d articles, checksum %s\n", c.Name, dir, len(c.Articles), c.Checksum)
	return 0
}
