package main

import (
	"context"
	"fmt"
	"github.com/sourcednet/resolver/cmdutil"
	"io"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/sourcednet/lab/bench"
)

func benchRun(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := cmdutil.NewFlags("bench run", "<experiment.yaml>", stderr)
	out := fs.String("out", filepath.Join("bench", "results"), "where to write the result")
	logs := cmdutil.AddLogFlags(fs, "also write progress to this file")
	if code, ok := cmdutil.ParseFlags(fs, args); !ok {
		return code
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return 2
	}
	log, closeLog, err := logs.Open(stderr, "")
	if err != nil {
		return cmdutil.Fail(stderr, err)
	}
	defer closeLog()
	cfg, err := bench.LoadConfig(fs.Arg(0))
	if err != nil {
		return cmdutil.Fail(stderr, err)
	}
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	res, err := bench.Run(ctx, cfg, log)
	if err != nil {
		return cmdutil.Fail(stderr, err)
	}
	path, err := res.Write(*out)
	if err != nil {
		return cmdutil.Fail(stderr, err)
	}
	fmt.Fprintf(stdout, "Result: %s\n", path)
	for _, w := range res.Warnings {
		fmt.Fprintln(stdout, "warning:", w)
	}
	return 0
}

func benchCompare(_ context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) != 2 {
		fmt.Fprint(stderr, benchUsage)
		return 2
	}
	a, err := bench.LoadResult(args[0])
	if err != nil {
		return cmdutil.Fail(stderr, err)
	}
	b, err := bench.LoadResult(args[1])
	if err != nil {
		return cmdutil.Fail(stderr, err)
	}
	bench.Compare(stdout, a, b)
	return 0
}
