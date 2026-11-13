package photos

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// LocalDir reads photos from a folder on the Pi (or a mounted network share).
// Folder ids are paths relative to the root; "." is the root itself.
type LocalDir struct {
	root string
	// Videos lists video files along with the photos.
	Videos bool
}

// NewLocalDir returns a source rooted at dir.
func NewLocalDir(dir string) *LocalDir { return &LocalDir{root: filepath.Clean(dir)} }

// Label implements Source.
func (l *LocalDir) Label() string { return "Folder " + l.root }

// HasPreviews implements Source.
func (l *LocalDir) HasPreviews() bool { return false }

// DefaultSelection implements Source: everything under the root.
func (l *LocalDir) DefaultSelection() Selection {
	return Selection{Recursive: true, Folders: []Folder{l.rootFolder()}}
}

func (l *LocalDir) rootFolder() Folder {
	return Folder{ID: ".", Name: filepath.Base(l.root), Path: filepath.Base(l.root)}
}

// abs turns a folder id into a path, refusing anything outside the root.
func (l *LocalDir) abs(id string) (string, error) {
	if id == "" || filepath.IsAbs(id) {
		return "", fmt.Errorf("bad folder id %q", id)
	}
	clean := filepath.Clean(id)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("bad folder id %q", id)
	}
	return filepath.Join(l.root, clean), nil
}

func hidden(name string) bool {
	// Dot files, and the metadata folders Synology and Windows leave on shares.
	return strings.HasPrefix(name, ".") || strings.HasPrefix(name, "@") || name == "#recycle"
}

// Folders implements Source.
func (l *LocalDir) Folders(_ context.Context, parent string) ([]Folder, error) {
	if parent == "" {
		if _, err := os.Stat(l.root); err != nil {
			return nil, err
		}
		return []Folder{l.rootFolder()}, nil
	}
	dir, err := l.abs(parent)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []Folder
	for _, e := range entries {
		if hidden(e.Name()) {
			continue
		}
		// Follow symlinks to directories, as on mounted shares.
		fi, err := os.Stat(filepath.Join(dir, e.Name()))
		if err != nil || !fi.IsDir() {
			continue
		}
		rel := filepath.Join(filepath.Clean(parent), e.Name())
		out = append(out, Folder{
			ID:   rel,
			Name: e.Name(),
			Path: filepath.Base(l.root) + " / " + strings.ReplaceAll(filepath.ToSlash(rel), "/", " / "),
		})
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}

// Items implements Source.
func (l *LocalDir) Items(_ context.Context, folder Folder) ([]Item, error) {
	dir, err := l.abs(folder.ID)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []Item
	for _, e := range entries {
		video := l.Videos && isVideoName(e.Name())
		if hidden(e.Name()) || !(decodable(e.Name()) || video) {
			continue
		}
		fi, err := os.Stat(filepath.Join(dir, e.Name()))
		if err != nil || !fi.Mode().IsRegular() {
			continue
		}
		rel := filepath.Join(filepath.Clean(folder.ID), e.Name())
		sum := sha1.Sum([]byte(rel))
		out = append(out, Item{
			Key:     "l-" + hex.EncodeToString(sum[:10]),
			Name:    e.Name(),
			Folder:  folder.Path,
			Taken:   fi.ModTime().UTC(),
			Version: fmt.Sprintf("%d-%d", fi.ModTime().Unix(), fi.Size()),
			Video:   video,
			Size:    fi.Size(),
			ID:      rel,
		})
	}
	return out, nil
}

// isVideoName reports files a browser may be able to play as video.
func isVideoName(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".mp4", ".m4v", ".mov", ".webm":
		return true
	}
	return false
}

// FilePath implements FilePather: videos are played from where they are.
func (l *LocalDir) FilePath(item Item) (string, error) { return l.abs(item.ID) }

// Peek implements Peeker: the start of the file holds its size and rotation.
// When that is not enough, the caller reads the whole file instead.
func (l *LocalDir) Peek(_ context.Context, item Item) ([]byte, bool, error) {
	p, err := l.abs(item.ID)
	if err != nil {
		return nil, false, err
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 256<<10))
	return data, false, err
}

// Fetch implements Source.
func (l *LocalDir) Fetch(_ context.Context, item Item, _ bool) ([]byte, bool, error) {
	p, err := l.abs(item.ID)
	if err != nil {
		return nil, false, err
	}
	fi, err := os.Stat(p)
	if err != nil {
		return nil, false, err
	}
	if fi.Size() > 100<<20 {
		return nil, false, errors.New("file is too large")
	}
	data, err := os.ReadFile(p)
	return data, false, err
}
