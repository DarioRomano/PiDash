// Package docs holds no program: this test keeps the guide in one piece.
//
// The guide is the files in docs/wiki, which a workflow publishes to the
// project's wiki. Its pages point at one another and at files of the
// repository, and the README, the project page and the installers point
// into it. A page that is renamed, or a heading that is reworded, must not
// leave any of those links leading nowhere.
package docs

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode"
)

const wikiURL = "https://github.com/DarioRomano/PiDash/wiki"

// anchor is the name GitHub gives a heading in a page's address.
func anchor(title string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(title) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_':
			b.WriteRune(r)
		case r == ' ':
			b.WriteRune('-')
		}
	}
	return b.String()
}

// page is one page of the guide: the anchors of its headings, and its text.
type page struct {
	anchors map[string]bool
	text    string
}

func readWiki(t *testing.T) map[string]page {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("wiki", "*.md"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no pages in docs/wiki: %v", err)
	}
	heading := regexp.MustCompile(`^#{1,6} (.+)$`)
	pages := map[string]page{}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		p := page{anchors: map[string]bool{}, text: string(data)}
		seen := map[string]int{}
		fenced := false
		for _, line := range strings.Split(p.text, "\n") {
			if strings.HasPrefix(line, "```") {
				fenced = !fenced
			}
			if m := heading.FindStringSubmatch(line); m != nil && !fenced {
				a := anchor(strings.TrimSpace(m[1]))
				// A second heading of the same name gets a number.
				if n := seen[a]; n > 0 {
					p.anchors[a+"-"+string(rune('0'+n))] = true
				} else {
					p.anchors[a] = true
				}
				seen[a]++
			}
		}
		pages[strings.TrimSuffix(filepath.Base(f), ".md")] = p
	}
	return pages
}

var link = regexp.MustCompile(`\]\(([^)\s]+)\)`)

func TestTheGuidesLinksLeadSomewhere(t *testing.T) {
	pages := readWiki(t)
	for name, p := range pages {
		for _, m := range link.FindAllStringSubmatch(p.text, -1) {
			target := m[1]
			switch {
			case strings.HasPrefix(target, "#"):
				if !p.anchors[target[1:]] {
					t.Errorf("%s: no heading for %s", name, target)
				}
			case strings.HasPrefix(target, "https://github.com/DarioRomano/PiDash/blob/main/"), strings.HasPrefix(target, "https://github.com/DarioRomano/PiDash/tree/main/"):
				file := strings.SplitN(target[len("https://github.com/DarioRomano/PiDash/blob/main/"):], "#", 2)[0]
				if _, err := os.Stat(filepath.Join("..", file)); err != nil {
					t.Errorf("%s: links to %s, which is not in the repository", name, file)
				}
			case strings.HasPrefix(target, "https://raw.githubusercontent.com/DarioRomano/PiDash/main/"):
				file := target[len("https://raw.githubusercontent.com/DarioRomano/PiDash/main/"):]
				if _, err := os.Stat(filepath.Join("..", file)); err != nil {
					t.Errorf("%s: shows %s, which is not in the repository", name, file)
				}
			case strings.Contains(target, "://"), strings.HasPrefix(target, "mailto:"):
				// Somewhere else on the internet: not this test's to say.
			default:
				// Another page of the guide, perhaps at one of its headings.
				to, at, _ := strings.Cut(target, "#")
				other, ok := pages[to]
				if !ok {
					t.Errorf("%s: links to the page %q, which there is not", name, to)
				} else if at != "" && !other.anchors[at] {
					t.Errorf("%s: %s has no heading for #%s", name, to, at)
				}
			}
		}
	}
	// Every page can be reached: from the sidebar, and from the first page.
	for name := range pages {
		if strings.HasPrefix(name, "_") || name == "Home" {
			continue
		}
		for _, from := range []string{"_Sidebar", "Home"} {
			if !strings.Contains(pages[from].text, "]("+name+")") {
				t.Errorf("%s does not lead to the page %s", from, name)
			}
		}
	}
}

func TestWhatPointsIntoTheGuideFindsItsPage(t *testing.T) {
	pages := readWiki(t)
	into := regexp.MustCompile(regexp.QuoteMeta(wikiURL) + `/([A-Za-z0-9_-]+)(?:#([A-Za-z0-9_-]+))?`)
	for _, file := range []string{"../README.md", "../site/index.html", "../install.sh", "../deploy/install.sh", "../config.example.toml",
		"../internal/server/whatsnew.go", "../web/settings.html", "../web/src/settings.ts"} {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range into.FindAllStringSubmatch(string(data), -1) {
			p, ok := pages[m[1]]
			if !ok {
				t.Errorf("%s points at the page %q of the guide, which there is not", file, m[1])
			} else if m[2] != "" && !p.anchors[m[2]] {
				t.Errorf("%s points at #%s in %s, which has no such heading", file, m[2], m[1])
			}
		}
	}
}

func TestTheReadmeStaysShortAndItsPicturesExist(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	// It is what someone sees first; what they may want to know next is in
	// the guide. If this fails, the new text belongs on one of its pages.
	if lines := strings.Count(string(data), "\n"); lines > 150 {
		t.Errorf("the README has grown to %d lines; it is kept under 150", lines)
	}
	shown := 0
	for _, m := range regexp.MustCompile(`\]\(([^)\s]+)\)`).FindAllStringSubmatch(string(data), -1) {
		target := m[1]
		if strings.Contains(target, "://") || strings.HasPrefix(target, "#") {
			continue
		}
		if _, err := os.Stat(filepath.Join("..", target)); err != nil {
			t.Errorf("the README links to %s, which is not in the repository", target)
		}
		if strings.HasSuffix(target, ".jpg") || strings.HasSuffix(target, ".png") {
			shown++
		}
	}
	if shown < 3 {
		t.Errorf("the README shows %d pictures; it is meant to show what PiDash looks like", shown)
	}
}
