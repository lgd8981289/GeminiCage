package main

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"math"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

type source struct {
	Title string `json:"title"`
	URL   string `json:"url"`
	Text  string `json:"-"`
}
type section struct {
	source
	doc bool
}
type index struct {
	sections []section
	terms    map[string][]int
}

var words = regexp.MustCompile(`[\p{Han}]+|[a-zA-Z0-9][a-zA-Z0-9_.+-]*`)

func tokens(text string) map[string]bool {
	result := map[string]bool{}
	for _, w := range words.FindAllString(strings.ToLower(text), -1) {
		r := []rune(w)
		if unicode.Is(unicode.Han, r[0]) {
			for j := 1; j < len(r); j++ {
				result[string(r[j-1:j+1])] = true
			}
		} else if len(r) > 1 {
			result[w] = true
		}
	}
	return result
}
func loadIndex(archive, rev string) (*index, error) {
	f, err := os.Open(archive)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	ix := &index{terms: map[string][]int{}}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		p := strings.TrimPrefix(h.Name, "source/")
		doc := p == "README.md" || strings.HasPrefix(p, "docs/guide/")
		if h.Typeflag != tar.TypeReg || h.Size > 512000 || strings.Contains(strings.ToLower(p), "test") {
			continue
		}
		switch path.Ext(p) {
		case ".go", ".swift", ".cs", ".ts", ".tsx", ".js", ".sh", ".kt":
		case ".md":
			if !doc {
				continue
			}
		default:
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return nil, err
		}
		lines := strings.Split(string(data), "\n")
		title := p
		for a := 0; a < len(lines); {
			b := a
			size := 0
			for b < len(lines) && size < 2100 && b-a < 40 {
				if b > a && doc && strings.HasPrefix(lines[b], "## ") {
					break
				}
				size += len(lines[b]) + 1
				b++
			}
			body := strings.TrimSpace(strings.Join(lines[a:b], "\n"))
			if r := []rune(body); len(r) > 2400 {
				body = string(r[:2400])
			}
			if strings.HasPrefix(lines[a], "## ") {
				title = strings.TrimPrefix(lines[a], "## ")
			}
			url := fmt.Sprintf("https://github.com/Lee-xeo/Claude-Cage/blob/%s/%s#L%d-L%d", rev, p, a+1, b)
			if strings.HasPrefix(p, "docs/guide/") {
				slug := strings.TrimSuffix(path.Base(p), ".md")
				url = "https://claude-cage.com/docs/"
				if slug != "index" {
					url += slug + "/"
				}
			}
			if len(body) > 35 {
				id := len(ix.sections)
				ix.sections = append(ix.sections, section{source{title, url, body}, doc})
				for w := range tokens(p + " " + title + " " + body) {
					ix.terms[w] = append(ix.terms[w], id)
				}
			}
			a = b
		}
	}
	if len(ix.sections) == 0 {
		return nil, fmt.Errorf("empty corpus")
	}
	return ix, nil
}
func (ix *index) search(q string) []source {
	scores := map[int]float64{}
	for w := range tokens(q) {
		ids := ix.terms[w]
		idf := math.Log(1 + float64(len(ix.sections))/float64(len(ids)+1))
		for _, id := range ids {
			scores[id] += idf
		}
	}
	ids := make([]int, 0, len(scores))
	for id := range scores {
		if ix.sections[id].doc {
			scores[id] *= 1.8
		}
		ids = append(ids, id)
	}
	sort.Slice(ids, func(a, b int) bool {
		i, j := ids[a], ids[b]
		if scores[i] == scores[j] {
			return i < j
		}
		return scores[i] > scores[j]
	})
	result := []source{}
	seen := map[string]int{}
	for _, id := range ids {
		s := ix.sections[id]
		if seen[s.URL] >= 2 {
			continue
		}
		seen[s.URL]++
		result = append(result, s.source)
		if len(result) == 6 {
			break
		}
	}
	return result
}
