package videos

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// Local is folders on the Pi: its own disk, a USB drive, or a share of the
// NAS that is mounted there.
type Local struct {
	// Roots are the folders, as full paths.
	Roots []string
}

// A place is written "<number of the folder>:<path inside it>".
func (l *Local) resolve(place string) (root int, rel, abs string, err error) {
	n, rest, ok := strings.Cut(place, ":")
	root, convErr := strconv.Atoi(n)
	if !ok || convErr != nil || root < 0 || root >= len(l.Roots) {
		return 0, "", "", ErrNotFound
	}
	rel = path.Clean("/" + rest)[1:]
	if rel != "" && !filepath.IsLocal(filepath.FromSlash(rel)) {
		return 0, "", "", ErrNotFound
	}
	return root, rel, filepath.Join(l.Roots[root], filepath.FromSlash(rel)), nil
}

func place(root int, rel string) string { return strconv.Itoa(root) + ":" + rel }

// List implements Source.
func (l *Local) List(_ context.Context, folder string) (title, parent string, top bool, entries []Entry, err error) {
	if folder == "" {
		if len(l.Roots) != 1 {
			// Several folders: the top lists them.
			for i, r := range l.Roots {
				entries = append(entries, Entry{Name: filepath.Base(r), Path: place(i, ""), Folder: true})
			}
			return "", "", true, entries, nil
		}
		folder = place(0, "")
	}
	root, rel, abs, err := l.resolve(folder)
	if err != nil {
		return "", "", false, nil, err
	}
	list, err := os.ReadDir(abs)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", "", false, nil, ErrNotFound
		}
		return "", "", false, nil, err
	}
	for _, d := range list {
		name := d.Name()
		if skipped(name) {
			continue
		}
		at := place(root, path.Join(rel, name))
		// A link is followed: it may lead to a folder on another disk.
		info, err := os.Stat(filepath.Join(abs, name))
		if err != nil {
			continue
		}
		switch {
		case info.IsDir():
			entries = append(entries, Entry{Name: name, Path: at, Folder: true})
		case IsVideo(name):
			entries = append(entries, Entry{Name: name, Path: at, Size: info.Size()})
		case isSubtitle(name):
			entries = append(entries, Entry{Name: name, Path: at, subtitle: true})
		case isPicture(name):
			entries = append(entries, Entry{Name: name, Path: at, picture: true})
		}
	}
	arrange(entries)
	title = filepath.Base(abs)
	switch {
	case rel != "":
		return title, place(root, path.Dir("/" + rel)[1:]), false, entries, nil
	case len(l.Roots) == 1:
		return title, "", true, entries, nil
	}
	return title, "", false, entries, nil
}

// Target implements Source: the file itself.
func (l *Local) Target(_ context.Context, file string) (string, error) {
	_, rel, abs, err := l.resolve(file)
	if err != nil || rel == "" {
		return "", ErrNotFound
	}
	return abs, nil
}

// Above implements Source.
func (l *Local) Above(file string) string {
	root, rel, _, err := l.resolve(file)
	if err != nil {
		return ""
	}
	return place(root, path.Dir("/" + rel)[1:])
}
