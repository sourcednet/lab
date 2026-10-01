package main

import (
	"context"

	"github.com/sourcednet/resolver"
	"github.com/sourcednet/resolver/sourcedmcp"
)

// pendingBackend lets an MCP client connect before the demo is ready: tool
// calls wait until the backend is set (or preparation failed), so a slow
// first start doesn't trip the client's connection timeout.
type pendingBackend struct {
	ready   chan struct{}
	backend sourcedmcp.Backend
	err     error
}

func newPendingBackend() *pendingBackend { return &pendingBackend{ready: make(chan struct{})} }

// resolve sets the backend, or the error that prevented it, once.
func (p *pendingBackend) resolve(b sourcedmcp.Backend, err error) {
	p.backend, p.err = b, err
	close(p.ready)
}

func (p *pendingBackend) wait(ctx context.Context) (sourcedmcp.Backend, error) {
	select {
	case <-p.ready:
		return p.backend, p.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (p *pendingBackend) FetchAnswer(ctx context.Context, req resolver.FetchRequest) (*resolver.FetchAnswer, error) {
	b, err := p.wait(ctx)
	if err != nil {
		return nil, err
	}
	return b.FetchAnswer(ctx, req)
}

func (p *pendingBackend) ResolveAnswer(ctx context.Context, citation string) (*resolver.ResolveAnswer, error) {
	b, err := p.wait(ctx)
	if err != nil {
		return nil, err
	}
	return b.ResolveAnswer(ctx, citation)
}

func (p *pendingBackend) VerifyAnswer(ctx context.Context, passage, pageURL string) (any, error) {
	b, err := p.wait(ctx)
	if err != nil {
		return nil, err
	}
	return b.VerifyAnswer(ctx, passage, pageURL)
}

func (p *pendingBackend) SearchAnswer(ctx context.Context, req resolver.SearchRequest) (*resolver.SearchAnswer, error) {
	b, err := p.wait(ctx)
	if err != nil {
		return nil, err
	}
	return b.SearchAnswer(ctx, req)
}
