package bench

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const attributionFile = "ATTRIBUTION.md"

// writeAttribution writes the credit and license notice that CC BY-SA asks
// for when the corpus is shared: what it is, who wrote it, under what
// license, and what was changed. Authors are credited through each
// article's history page, as Wikipedia's reuse guidelines suggest; no
// usernames are stored.
func (c *Corpus) writeAttribution() error {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s: attribution and license\n\n", c.Name)
	fmt.Fprintf(&b, "This dataset holds %d Wikipedia articles, each with its %d latest consecutive revisions, fetched on %s from %s.\n\n",
		len(c.Articles), c.revisionsPerArticle(), c.Fetched.Format("2006-01-02"), c.Source)
	b.WriteString("## License\n\n")
	b.WriteString("The text is by the Wikipedia contributors and is licensed under the Creative Commons Attribution-ShareAlike 4.0 International license (CC BY-SA 4.0): https://creativecommons.org/licenses/by-sa/4.0/. ")
	b.WriteString("You may share and adapt it, if you credit the authors, link to the license, say what you changed, and share your adaptations under the same license. This dataset as a whole is offered under CC BY-SA 4.0.\n\n")
	b.WriteString("## Authors\n\n")
	b.WriteString("Each article's authors are listed in its revision history, linked below. The dataset stores no usernames.\n\n")
	b.WriteString("## Changes\n\n")
	b.WriteString("Each revision's HTML is the article content as rendered by the MediaWiki API (`action=parse`, with section edit links and the table of contents turned off), stored unmodified and gzipped. ")
	b.WriteString("`corpus.json` holds each revision's ID, timestamp, edit summary, minor-edit flag, and size, as the API reported them. `CHECKSUM` covers every file.\n\n")
	b.WriteString("## Articles\n\n")
	b.WriteString("Each revision can be viewed at its permanent link, `https://<site>/w/index.php?oldid=<revision>`.\n\n")
	for _, a := range c.Articles {
		u, err := url.Parse(a.URL)
		if err != nil {
			return err
		}
		base := u.Scheme + "://" + u.Host
		history := base + "/w/index.php?title=" + url.QueryEscape(a.Title) + "&action=history"
		var ids []string
		for _, r := range a.Revisions {
			ids = append(ids, fmt.Sprint(r.ID))
		}
		fmt.Fprintf(&b, "- [%s](%s): [history and authors](%s); revisions %s", a.Title, a.URL, history, strings.Join(ids, ", "))
		if len(a.HiddenSkipped) > 0 {
			var hidden []string
			for _, id := range a.HiddenSkipped {
				hidden = append(hidden, fmt.Sprint(id))
			}
			fmt.Fprintf(&b, " (left out, hidden by Wikipedia: %s)", strings.Join(hidden, ", "))
		}
		b.WriteString("\n")
	}
	return os.WriteFile(filepath.Join(c.dir, attributionFile), []byte(b.String()), 0o644)
}

func (c *Corpus) revisionsPerArticle() int {
	if len(c.Articles) == 0 {
		return 0
	}
	return len(c.Articles[0].Revisions)
}

// Reseal rewrites a corpus's attribution file and checksum from its
// corpus.json, e.g. after a fetch by an older version of this tool.
func Reseal(dir string) (*Corpus, error) {
	b, err := os.ReadFile(filepath.Join(dir, corpusFile))
	if err != nil {
		return nil, err
	}
	var c Corpus
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	c.dir = dir
	return &c, c.seal()
}
