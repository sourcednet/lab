package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sourcednet/resolver"
)

// stubBackend answers every call with an empty answer.
type stubBackend struct{}

func (stubBackend) FetchAnswer(context.Context, resolver.FetchRequest) (*resolver.FetchAnswer, error) {
	return &resolver.FetchAnswer{}, nil
}

func (stubBackend) ResolveAnswer(context.Context, string) (*resolver.ResolveAnswer, error) {
	return &resolver.ResolveAnswer{}, nil
}

func (stubBackend) VerifyAnswer(context.Context, string, string) (any, error) {
	return &resolver.VerifyAnswer{}, nil
}

func (stubBackend) SearchAnswer(context.Context, resolver.SearchRequest) (*resolver.SearchAnswer, error) {
	return &resolver.SearchAnswer{}, nil
}

func TestPendingBackendWaitsUntilReady(t *testing.T) {
	p := newPendingBackend()
	done := make(chan error, 1)
	go func() {
		_, err := p.FetchAnswer(context.Background(), resolver.FetchRequest{URL: "https://x.test/"})
		done <- err
	}()
	select {
	case <-done:
		t.Fatal("call returned before the backend was ready")
	case <-time.After(20 * time.Millisecond):
	}
	p.resolve(stubBackend{}, nil)
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	failed := newPendingBackend()
	failed.resolve(nil, errors.New("sample sites missing"))
	if _, err := failed.SearchAnswer(context.Background(), resolver.SearchRequest{Query: "q"}); err == nil || err.Error() != "sample sites missing" {
		t.Fatalf("preparation error not passed on: %v", err)
	}
}

func TestPrepareSiteIsAtomic(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".site.test.preparing", "junk"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := prepareSite(dir, "site.test", func(tmp string) error {
		os.MkdirAll(filepath.Join(tmp, "public"), 0o755)
		return errors.New("build failed halfway")
	})
	if err == nil {
		t.Fatal("want the build error")
	}
	for _, p := range []string{"site.test", ".site.test.preparing"} {
		if _, err := os.Stat(filepath.Join(dir, p)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s left behind after a failed build", p)
		}
	}
	if err := prepareSite(dir, "site.test", func(tmp string) error { return os.MkdirAll(filepath.Join(tmp, "public"), 0o755) }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "site.test", "public")); err != nil {
		t.Fatalf("site not in place: %v", err)
	}
}
