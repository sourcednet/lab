package main

import (
	"bytes"
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sourcednet/resolver"
	"github.com/sourcednet/resolver/verifier"
)

func TestDemoPrepareThenServe(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo")
	if err := prepareDemoDir(context.Background(), prepareOptions{Dir: dir}, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatal(err)
	}
	for _, local := range []bool{false, true} {
		var logs bytes.Buffer
		d, err := startDemo(context.Background(), demoOptions{Dir: dir, Local: local}, slog.New(slog.NewTextHandler(&logs, nil)))
		if err != nil {
			t.Fatal(err)
		}
		a, err := d.backend.FetchAnswer(context.Background(), resolver.FetchRequest{URL: "https://daily-herald.test/news/2026/09/bridge-reopens.html"})
		d.Close()
		if err != nil || a.Verification != verifier.Verified || len(d.domains) != 5 {
			t.Fatalf("local=%v: %+v, %v, domains %v", local, a, err, d.domains)
		}
		// Serving a prepared folder does no sync work: every publisher is unchanged.
		if !local && (strings.Contains(logs.String(), `result=updated`) || !strings.Contains(logs.String(), `result="not modified"`)) {
			t.Fatalf("serving re-synced instead of checking for changes:\n%s", logs.String())
		}
	}
}

func TestDemoNeedsPreparing(t *testing.T) {
	quiet := slog.New(slog.DiscardHandler)
	if err := prepareDemoDir(context.Background(), prepareOptions{Dir: filepath.Join(t.TempDir(), "demo"), Sites: "no-such-dir"}, quiet); err == nil {
		t.Fatal("want an error for a missing sites directory")
	}
	_, err := startDemo(context.Background(), demoOptions{Dir: filepath.Join(t.TempDir(), "never-prepared")}, quiet)
	if err == nil || !strings.Contains(err.Error(), "sourced-lab demo prepare") {
		t.Fatalf("serving an unprepared folder: %v", err)
	}
}
