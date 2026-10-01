package bench

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeWikipedia answers the two API calls the fetcher makes, and asks it to
// slow down (maxlag) once.
func fakeWikipedia(t *testing.T, lagged *atomic.Bool, ua *atomic.Value) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua.Store(r.Header.Get("User-Agent"))
		q := r.URL.Query()
		if !lagged.Load() {
			lagged.Store(true)
			w.Header().Set("Retry-After", "1")
			fmt.Fprint(w, `{"error":{"code":"maxlag","info":"Waiting for a database server"}}`)
			return
		}
		switch q.Get("action") {
		case "query":
			if q.Get("titles") == "Nope" {
				fmt.Fprint(w, `{"query":{"pages":[{"title":"Nope","missing":true}]}}`)
				return
			}
			fmt.Fprintf(w, `{"query":{"pages":[{"title":%q,"revisions":[
				{"revid":3,"timestamp":"2026-09-03T00:00:00Z","comment":"third","size":30},
				{"revid":2,"timestamp":"2026-09-02T00:00:00Z","comment":"second","minor":true,"size":20},
				{"revid":1,"timestamp":"2026-09-01T00:00:00Z","comment":"first","size":10}]}]}}`, q.Get("titles"))
		case "parse":
			if q.Get("titles") == "" && q.Get("oldid") == "2" && r.Header.Get("X-Hide") == "1" {
				fmt.Fprint(w, `{"error":{"code":"permissiondenied","info":"You don't have permission to view deleted text"}}`)
				return
			}
			fmt.Fprintf(w, `{"parse":{"title":"T","text":"<div class=\"mw-parser-output\"><p>Revision %s text.</p></div>"}}`, q.Get("oldid"))
		}
	}))
}

func TestFetchAndSeal(t *testing.T) {
	var lagged atomic.Bool
	var ua atomic.Value
	srv := fakeWikipedia(t, &lagged, &ua)
	defer srv.Close()
	// Send every request to the fake server, whatever the host.
	u, _ := url.Parse(srv.URL)
	client := &http.Client{Transport: rewrite{u}}

	def := &CorpusDef{Name: "test", Lang: "en", Revisions: 3, Titles: []string{"Black hole", "Coffee"}}
	dir := t.TempDir()
	f := &Fetcher{HTTP: client, Delay: time.Millisecond}
	c, err := f.Fetch(context.Background(), def, dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ua.Load().(string), "github.com/sourcednet/lab") {
		t.Fatalf("user agent %q", ua.Load())
	}
	if len(c.Articles) != 2 || len(c.Articles[0].Revisions) != 3 || c.Articles[0].Revisions[0].ID != 1 {
		t.Fatalf("corpus: %+v", c.Articles)
	}

	loaded, err := LoadCorpus(dir)
	if err != nil {
		t.Fatal(err)
	}
	html, err := loaded.HTML(loaded.Articles[0].Revisions[2])
	if err != nil || !strings.Contains(string(html), "Revision 3 text.") {
		t.Fatalf("html %q, %v", html, err)
	}
	if loaded.Checksum != c.Checksum {
		t.Fatal("checksum changed on load")
	}
	attr, err := os.ReadFile(dir + "/ATTRIBUTION.md")
	if err != nil || !strings.Contains(string(attr), "CC BY-SA 4.0") || !strings.Contains(string(attr), "title=Black+hole&action=history") {
		t.Fatalf("attribution: %v\n%s", err, attr)
	}

	// A second fetch reuses everything: no HTML is fetched again.
	srv.Close()
	if _, err := (&Fetcher{HTTP: client, Delay: time.Millisecond}).Fetch(context.Background(), def, dir); err != nil {
		t.Fatalf("resume should not need the network: %v", err)
	}

	// Tampering is caught.
	if err := writeGzip(dir+"/"+loaded.Articles[0].Revisions[0].File, []byte("changed")); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCorpus(dir); err == nil || !strings.Contains(err.Error(), "changed since it was sealed") {
		t.Fatalf("tampered corpus: %v", err)
	}
}

func TestFetchMissingPage(t *testing.T) {
	var lagged atomic.Bool
	lagged.Store(true)
	var ua atomic.Value
	srv := fakeWikipedia(t, &lagged, &ua)
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	_, err := (&Fetcher{HTTP: &http.Client{Transport: rewrite{u}}, Delay: time.Millisecond}).Fetch(context.Background(),
		&CorpusDef{Name: "t", Lang: "en", Revisions: 3, Titles: []string{"Nope"}}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "page not found") {
		t.Fatalf("got %v", err)
	}
}

type rewrite struct{ to *url.URL }

func (r rewrite) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme, req.URL.Host = r.to.Scheme, r.to.Host
	return http.DefaultTransport.RoundTrip(req)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFetchSkipsHiddenRevisions(t *testing.T) {
	var lagged atomic.Bool
	lagged.Store(true)
	var ua atomic.Value
	srv := fakeWikipedia(t, &lagged, &ua)
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	client := &http.Client{Transport: hide{rewrite{u}}}
	c, err := (&Fetcher{HTTP: client, Delay: time.Millisecond}).Fetch(context.Background(),
		&CorpusDef{Name: "t", Lang: "en", Revisions: 3, Titles: []string{"Coffee"}}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := c.Articles[0]
	if len(a.Revisions) != 2 || len(a.HiddenSkipped) != 1 || a.HiddenSkipped[0] != 2 {
		t.Fatalf("got %d revisions, hidden %v", len(a.Revisions), a.HiddenSkipped)
	}
}

// hide marks requests so the fake API hides revision 2.
type hide struct{ rt http.RoundTripper }

func (h hide) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("X-Hide", "1")
	return h.rt.RoundTrip(req)
}

func TestFetchStopsWhenCanceled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done() // never answers
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := (&Fetcher{HTTP: &http.Client{Transport: rewrite{u}}, Delay: time.Millisecond}).Fetch(ctx,
		&CorpusDef{Name: "t", Lang: "en", Revisions: 3, Titles: []string{"Coffee"}}, t.TempDir())
	if err == nil || time.Since(start) > 2*time.Second {
		t.Fatalf("canceled fetch: err %v after %s; want a prompt error, no retries", err, time.Since(start))
	}
}
