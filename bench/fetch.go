package bench

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"time"
)

// UserAgent identifies the fetcher to Wikimedia, as their API etiquette asks.
const UserAgent = "sourcednet-bench/0.1 (https://github.com/sourcednet/lab; research benchmark, one-time fetch)"

// Fetcher downloads a corpus from Wikipedia politely: one request at a
// time, a pause between requests, Wikimedia's maxlag signal honored, and
// nothing fetched twice (an interrupted run resumes).
type Fetcher struct {
	HTTP  *http.Client
	Delay time.Duration // between requests; default 500ms
	Log   *slog.Logger

	last time.Time
}

// Fetch downloads def into dir and seals it.
func (f *Fetcher) Fetch(ctx context.Context, def *CorpusDef, dir string) (*Corpus, error) {
	if f.HTTP == nil {
		f.HTTP = &http.Client{Timeout: 60 * time.Second}
	}
	if f.Delay == 0 {
		f.Delay = 500 * time.Millisecond
	}
	if f.Log == nil {
		f.Log = slog.New(slog.DiscardHandler)
	}
	api := "https://" + def.Lang + ".wikipedia.org/w/api.php"
	c := &Corpus{
		Name:    def.Name,
		Source:  "https://" + def.Lang + ".wikipedia.org (MediaWiki action API: prop=revisions, action=parse)",
		License: "CC BY-SA 4.0, https://creativecommons.org/licenses/by-sa/4.0/ (Wikipedia contributors)",
		Fetched: time.Now().UTC().Truncate(time.Second),
		dir:     dir,
	}
	for i, title := range def.Titles {
		a, err := f.article(ctx, api, def, dir, title)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", title, err)
		}
		f.Log.Info("article", "n", i+1, "of", len(def.Titles), "title", a.Title, "revisions", len(a.Revisions))
		c.Articles = append(c.Articles, *a)
	}
	if err := c.seal(); err != nil {
		return nil, err
	}
	return c, nil
}

func (f *Fetcher) article(ctx context.Context, api string, def *CorpusDef, dir, title string) (*Article, error) {
	s := slug(title)
	meta := filepath.Join(dir, "articles", s, "meta.json")
	var a Article
	if b, err := os.ReadFile(meta); err == nil && json.Unmarshal(b, &a) == nil && len(a.Revisions) > 0 {
		// Already fetched; any HTML still missing is fetched below.
	} else {
		listed, err := f.listRevisions(ctx, api, title, def.Revisions)
		if err != nil {
			return nil, err
		}
		a = *listed
		a.Slug = s
		a.URL = "https://" + def.Lang + ".wikipedia.org/wiki/" + url.PathEscape(a.Title)
	}
	var kept []Revision
	for _, r := range a.Revisions {
		r.File = "articles/" + s + "/" + strconv.FormatInt(r.ID, 10) + ".html.gz"
		path := filepath.Join(dir, filepath.FromSlash(r.File))
		if !fileExists(path) {
			html, err := f.revisionHTML(ctx, api, r.ID)
			if errors.Is(err, errHidden) {
				f.Log.Info("revision hidden by Wikipedia, skipped", "title", a.Title, "revision", r.ID)
				a.HiddenSkipped = append(a.HiddenSkipped, r.ID)
				continue
			}
			if err != nil {
				return nil, fmt.Errorf("revision %d: %w", r.ID, err)
			}
			if err := writeGzip(path, html); err != nil {
				return nil, err
			}
		}
		kept = append(kept, r)
	}
	if len(kept) < 2 {
		return nil, errors.New("fewer than 2 public revisions")
	}
	a.Revisions = kept
	b, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(meta), 0o755); err != nil {
		return nil, err
	}
	return &a, os.WriteFile(meta, b, 0o644)
}

// listRevisions lists the latest n revisions of a page, oldest first.
func (f *Fetcher) listRevisions(ctx context.Context, api, title string, n int) (*Article, error) {
	q := url.Values{
		"action": {"query"}, "format": {"json"}, "formatversion": {"2"}, "redirects": {"1"},
		"prop": {"revisions"}, "titles": {title}, "rvlimit": {strconv.Itoa(n)},
		"rvprop": {"ids|timestamp|comment|flags|size"},
	}
	var resp struct {
		Query struct {
			Pages []struct {
				Title     string `json:"title"`
				Missing   bool   `json:"missing"`
				Revisions []struct {
					RevID     int64     `json:"revid"`
					Timestamp time.Time `json:"timestamp"`
					Comment   string    `json:"comment"`
					Minor     bool      `json:"minor"`
					Size      int       `json:"size"`
				} `json:"revisions"`
			} `json:"pages"`
		} `json:"query"`
	}
	if err := f.call(ctx, api, q, &resp); err != nil {
		return nil, err
	}
	if len(resp.Query.Pages) != 1 || resp.Query.Pages[0].Missing {
		return nil, errors.New("page not found")
	}
	p := resp.Query.Pages[0]
	if len(p.Revisions) < n {
		return nil, fmt.Errorf("only %d revisions, want %d", len(p.Revisions), n)
	}
	a := &Article{Title: p.Title}
	for _, r := range p.Revisions {
		a.Revisions = append(a.Revisions, Revision{ID: r.RevID, Timestamp: r.Timestamp, Comment: r.Comment, Minor: r.Minor, Size: r.Size})
	}
	slices.Reverse(a.Revisions) // the API lists newest first
	return a, nil
}

// revisionHTML returns the rendered HTML of one revision's content.
func (f *Fetcher) revisionHTML(ctx context.Context, api string, id int64) ([]byte, error) {
	q := url.Values{
		"action": {"parse"}, "format": {"json"}, "formatversion": {"2"},
		"oldid": {strconv.FormatInt(id, 10)}, "prop": {"text"},
		"disableeditsection": {"1"}, "disabletoc": {"1"},
	}
	var resp struct {
		Parse struct {
			Title string `json:"title"`
			Text  string `json:"text"`
		} `json:"parse"`
	}
	if err := f.call(ctx, api, q, &resp); err != nil {
		return nil, err
	}
	if resp.Parse.Text == "" {
		return nil, errors.New("empty parse result")
	}
	return []byte(resp.Parse.Text), nil
}

// slowDown is Wikipedia asking for fewer requests (429, maxlag, ratelimited).
type slowDown struct{ reason string }

func (e *slowDown) Error() string { return e.reason }

// errHidden means a revision's text was deleted from public view.
var errHidden = errors.New("revision text is hidden")

// call performs one API request, spaced from the previous one, retrying
// politely when Wikimedia asks to slow down.
func (f *Fetcher) call(ctx context.Context, api string, q url.Values, out any) error {
	q.Set("maxlag", "5")
	u := api + "?" + q.Encode()
	backoff := 5 * time.Second
	for attempt := 1; ; attempt++ {
		if wait := f.Delay - time.Since(f.last); wait > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
			}
		}
		f.last = time.Now()
		retry, err := f.once(ctx, u, out)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err() // stopped: don't retry, don't misreport
		}
		if retry == 0 || attempt >= 6 {
			return err
		}
		msg := "request failed, retrying"
		var slow *slowDown
		if errors.As(err, &slow) {
			msg = "wikipedia asked to slow down, waiting"
		}
		if retry < 0 {
			retry = backoff
			backoff *= 2
		}
		f.Log.Warn(msg, "wait", retry, "attempt", attempt, "err", err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(retry):
		}
	}
}

// once performs a request. On failure it returns how long to wait before
// retrying: 0 for no retry, negative for the default backoff.
func (f *Fetcher) once(ctx context.Context, u string, out any) (time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", UserAgent)
	resp, err := f.HTTP.Do(req)
	if err != nil {
		return -1, err
	}
	defer resp.Body.Close()
	retryAfter := time.Duration(-1)
	if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && s > 0 {
		retryAfter = time.Duration(s) * time.Second
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return retryAfter, &slowDown{"HTTP " + resp.Status}
	}
	if resp.StatusCode >= 500 {
		return retryAfter, fmt.Errorf("HTTP %s", resp.Status)
	}
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("HTTP %s", resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return -1, err
	}
	var apiErr struct {
		Error *struct {
			Code string `json:"code"`
			Info string `json:"info"`
		} `json:"error"`
	}
	if json.Unmarshal(b, &apiErr) == nil && apiErr.Error != nil {
		switch apiErr.Error.Code {
		case "maxlag", "ratelimited":
			return retryAfter, &slowDown{apiErr.Error.Code + ": " + apiErr.Error.Info}
		case "permissiondenied", "nosuchrevid":
			return 0, fmt.Errorf("%w: %s", errHidden, apiErr.Error.Info)
		}
		return 0, fmt.Errorf("%s: %s", apiErr.Error.Code, apiErr.Error.Info)
	}
	return 0, json.Unmarshal(b, out)
}
