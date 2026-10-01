package bench

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
)

// LoadResult reads a result file.
func LoadResult(path string) (*Result, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var r Result
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &r, nil
}

// Flatten turns a result's metrics into dotted names and numbers, e.g.
// "stability.survival_per_edit.mean". Lists (like per-step costs) are left
// out; their summaries are flattened.
func (r *Result) Flatten() map[string]float64 {
	b, _ := json.Marshal(r.Metrics)
	var v any
	json.Unmarshal(b, &v)
	out := map[string]float64{}
	flatten("", v, out)
	return out
}

func flatten(prefix string, v any, out map[string]float64) {
	switch x := v.(type) {
	case map[string]any:
		for k, sub := range x {
			name := k
			if prefix != "" {
				name = prefix + "." + k
			}
			flatten(name, sub, out)
		}
	case float64:
		out[prefix] = x
	}
}

// Compare writes two results side by side, with the change from a to b.
// It notes when they were computed on different corpora or code.
func Compare(w io.Writer, a, b *Result) {
	fmt.Fprintf(w, "A: %s (%s, commit %.12s)\nB: %s (%s, commit %.12s)\n", a.Name, a.Started.Format("2006-01-02 15:04"), a.Git.Commit, b.Name, b.Started.Format("2006-01-02 15:04"), b.Git.Commit)
	if a.Corpus.Checksum != b.Corpus.Checksum {
		fmt.Fprintln(w, "Note: different corpora; differences may not come from the config.")
	}
	if a.Git.Commit != b.Git.Commit || a.Git.Dirty || b.Git.Dirty {
		fmt.Fprintln(w, "Note: different or uncommitted code.")
	}
	if timed := append(slices.Clone(a.Timed), b.Timed...); len(timed) > 0 {
		fmt.Fprintf(w, "Note: %s measure wall-clock time and vary between runs.\n", strings.Join(slices.Compact(slices.Sorted(slices.Values(timed))), ", "))
	}
	fa, fb := a.Flatten(), b.Flatten()
	var keys []string
	for k := range fa {
		keys = append(keys, k)
	}
	for k := range fb {
		if _, ok := fa[k]; !ok {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	t := tabwriter.NewWriter(w, 0, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintln(t, "METRIC\tA\tB\tCHANGE\t")
	for _, k := range keys {
		va, oka := fa[k]
		vb, okb := fb[k]
		change := ""
		switch {
		case oka && okb && va != 0:
			change = fmt.Sprintf("%+.1f%%", (vb-va)/va*100)
		case oka && okb && vb != va:
			change = "new"
		}
		fmt.Fprintf(t, "%s\t%s\t%s\t%s\t\n", k, num(va, oka), num(vb, okb), change)
	}
	t.Flush()
}

func num(v float64, ok bool) string {
	if !ok {
		return "-"
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}
