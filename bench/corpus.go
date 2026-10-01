// Package bench is the benchmark harness: a frozen corpus of real revision
// histories, experiments run against it, and results that can be compared.
package bench

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// CorpusDef defines a corpus to fetch: which articles, and how many
// revisions of each.
type CorpusDef struct {
	Name      string   `yaml:"name"`
	Lang      string   `yaml:"lang"`
	Revisions int      `yaml:"revisions"`
	Titles    []string `yaml:"titles"`
}

// LoadCorpusDef reads a corpus definition file.
func LoadCorpusDef(path string) (*CorpusDef, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var d CorpusDef
	if err := yaml.Unmarshal(b, &d); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if d.Name == "" || d.Lang == "" || d.Revisions < 2 || len(d.Titles) == 0 {
		return nil, fmt.Errorf("%s: want a name, lang, revisions >= 2, and titles", path)
	}
	return &d, nil
}

// Corpus is a frozen snapshot: articles with consecutive revisions, oldest
// first. Revision HTML is stored gzipped next to corpus.json.
type Corpus struct {
	Name     string    `json:"name"`
	Source   string    `json:"source"`
	License  string    `json:"license"`
	Fetched  time.Time `json:"fetched"`
	Articles []Article `json:"articles"`

	dir      string
	Checksum string `json:"-"`
}

// Article is one page and its revisions, oldest first.
type Article struct {
	Title     string     `json:"title"`
	Slug      string     `json:"slug"`
	URL       string     `json:"url"`
	Revisions []Revision `json:"revisions"`
	// HiddenSkipped counts revisions in the range that Wikipedia has
	// deleted (usually vandalism or copyright); their text isn't public.
	HiddenSkipped []int64 `json:"hidden_skipped,omitempty"`
}

// Revision is one version of an article. No editor names are stored.
type Revision struct {
	ID        int64     `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Comment   string    `json:"comment,omitempty"`
	Minor     bool      `json:"minor,omitempty"`
	Size      int       `json:"size"`
	File      string    `json:"file"`
}

// HTML returns a revision's HTML.
func (c *Corpus) HTML(rev Revision) ([]byte, error) {
	f, err := os.Open(filepath.Join(c.dir, filepath.FromSlash(rev.File)))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	return io.ReadAll(zr)
}

const (
	corpusFile   = "corpus.json"
	checksumFile = "CHECKSUM"
)

// LoadCorpus opens a frozen corpus and checks that nothing in it changed.
func LoadCorpus(dir string) (*Corpus, error) {
	b, err := os.ReadFile(filepath.Join(dir, corpusFile))
	if err != nil {
		return nil, fmt.Errorf("corpus: %w (fetch it with `sourced-lab bench fetch`)", err)
	}
	var c Corpus
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("corpus: %w", err)
	}
	c.dir = dir
	want, err := os.ReadFile(filepath.Join(dir, checksumFile))
	if err != nil {
		return nil, fmt.Errorf("corpus is not sealed: %w", err)
	}
	got, err := checksum(dir)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(want)) != got {
		return nil, fmt.Errorf("corpus %s changed since it was sealed (checksum %s, sealed %s)", dir, got, strings.TrimSpace(string(want)))
	}
	c.Checksum = got
	return &c, nil
}

// seal writes corpus.json and the checksum over every file.
func (c *Corpus) seal() error {
	sort.Slice(c.Articles, func(i, j int) bool { return c.Articles[i].Slug < c.Articles[j].Slug })
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(c.dir, corpusFile), append(b, '\n'), 0o644); err != nil {
		return err
	}
	if err := c.writeAttribution(); err != nil {
		return err
	}
	sum, err := checksum(c.dir)
	if err != nil {
		return err
	}
	c.Checksum = sum
	return os.WriteFile(filepath.Join(c.dir, checksumFile), []byte(sum+"\n"), 0o644)
}

// checksum hashes every file in dir except the checksum itself, in path
// order: "sha256:<hex>".
func checksum(dir string) (string, error) {
	var paths []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && d.Name() != checksumFile && !strings.HasSuffix(d.Name(), ".part") {
			rel, _ := filepath.Rel(dir, p)
			paths = append(paths, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, p := range paths {
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(p)))
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%s\x00%d\x00", p, len(b))
		h.Write(b)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// slug turns a title into a file-safe name.
func slug(title string) string {
	return strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(title), "-"), "-")
}

func writeGzip(path string, b []byte) error {
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	zw.Write(b)
	if err := zw.Close(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".part"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return !errors.Is(err, fs.ErrNotExist)
}
