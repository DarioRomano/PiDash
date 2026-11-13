// Package photos provides the background slideshow: it indexes the photos in
// the folders the user picked, and serves each one as a ready-to-show
// FrameW x FrameH JPEG, rendered once and cached on disk.
package photos

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Folder is a place photos can be picked from.
type Folder struct {
	// ID identifies the folder to its source.
	ID string `json:"id"`
	// Name is the folder's own name; Path its full, human-readable location.
	Name string `json:"name"`
	Path string `json:"path"`
}

// Item is one photo.
type Item struct {
	// Key is unique, stable and URL-safe; it is the name in /photos/{key}.
	Key string `json:"key"`
	// Name is the file name; Folder the readable path of its folder.
	Name   string    `json:"name"`
	Folder string    `json:"folder"`
	Taken  time.Time `json:"taken"`
	// Version changes when the photo is edited, so stale renders are not reused.
	Version string `json:"-"`
	// ID is what the source needs to fetch the photo.
	ID string `json:"-"`
}

// Source is where photos come from: Synology Photos, or a folder on disk.
type Source interface {
	// Label names the source for humans, e.g. "Synology Photos".
	Label() string
	// Folders lists the folders inside parent; parent "" lists the top level.
	Folders(ctx context.Context, parent string) ([]Folder, error)
	// Items lists the photos directly inside a folder (not its subfolders).
	Items(ctx context.Context, folder Folder) ([]Item, error)
	// Fetch returns the encoded image. With original=false a source that has
	// ready-made smaller versions returns one of those instead of the full
	// file. upright reports that the image is already rotated correctly.
	Fetch(ctx context.Context, item Item, original bool) (data []byte, upright bool, err error)
	// HasPreviews reports whether original=false gives a different, smaller image.
	HasPreviews() bool
	// DefaultSelection is used until the user has picked folders; may be empty.
	DefaultSelection() Selection
}

// Selection is the user's choice of folders for the background.
type Selection struct {
	Folders []Folder `json:"folders"`
	// Recursive also includes everything inside the chosen folders' subfolders.
	Recursive bool `json:"recursive"`
}

// Status is sent to the dashboard and the folder picker.
type Status struct {
	Enabled bool   `json:"enabled"`
	Source  string `json:"source"`
	// Count is the number of photos currently in the slideshow.
	Count    int  `json:"count"`
	Indexing bool `json:"indexing"`
	// Error is the last problem reaching the source; old photos keep showing.
	Error           string    `json:"error,omitempty"`
	IntervalSeconds int       `json:"intervalSeconds"`
	Selection       Selection `json:"selection"`
	// Version increases whenever the set of photos changes.
	Version int `json:"version"`
}

// Options configure a Service.
type Options struct {
	Source Source
	// Interval between photos on screen; Refresh between re-reads of the folders.
	Interval time.Duration
	Refresh  time.Duration
	// PreferOriginal renders from the full file where possible (sharper) instead
	// of the source's smaller preview.
	PreferOriginal bool
	// StatePath stores the folder selection; CacheDir the rendered frames.
	StatePath  string
	CacheDir   string
	CacheBytes int64
	Log        *slog.Logger
}

const (
	maxFolders = 5000
	maxItems   = 200_000
)

// Service indexes the selected folders and serves rendered photos.
type Service struct {
	opt Options

	mu        sync.Mutex
	selection Selection
	items     []Item
	byKey     map[string]Item
	version   int
	indexing  bool
	lastErr   string
	onChange  func(Status)

	reindex chan struct{}

	renderSem chan struct{}
	flightMu  sync.Mutex
	flight    map[string]*flightCall
	writes    int
}

type flightCall struct {
	done chan struct{}
	path string
	err  error
}

// New creates a photo service. Call Run to start indexing.
func New(opt Options) *Service {
	if opt.Interval <= 0 {
		opt.Interval = 30 * time.Second
	}
	if opt.Refresh <= 0 {
		opt.Refresh = 6 * time.Hour
	}
	if opt.CacheBytes <= 0 {
		opt.CacheBytes = 2 << 30
	}
	s := &Service{
		opt:       opt,
		byKey:     map[string]Item{},
		onChange:  func(Status) {},
		reindex:   make(chan struct{}, 1),
		renderSem: make(chan struct{}, 1),
		flight:    map[string]*flightCall{},
	}
	// Loaded here rather than in Run, so a choice made right after start-up
	// can never be overwritten by the saved one.
	sel, ok := s.loadSelection()
	if !ok {
		sel = opt.Source.DefaultSelection()
	}
	s.selection = sel
	return s
}

// OnChange registers a callback for status changes.
func (s *Service) OnChange(fn func(Status)) {
	s.mu.Lock()
	s.onChange = fn
	s.mu.Unlock()
}

// Status returns the current state.
func (s *Service) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.statusLocked()
}

func (s *Service) statusLocked() Status {
	sel := s.selection
	if sel.Folders == nil {
		sel.Folders = []Folder{}
	}
	return Status{
		Enabled:         true,
		Source:          s.opt.Source.Label(),
		Count:           len(s.items),
		Indexing:        s.indexing,
		Error:           s.lastErr,
		IntervalSeconds: int(s.opt.Interval / time.Second),
		Selection:       sel,
		Version:         s.version,
	}
}

func (s *Service) notify() {
	s.mu.Lock()
	st, fn := s.statusLocked(), s.onChange
	s.mu.Unlock()
	fn(st)
}

// Run indexes now, whenever the selection changes, and periodically to pick up
// new photos. It returns when ctx ends.
func (s *Service) Run(ctx context.Context) {
	if s.opt.CacheDir != "" {
		_ = os.MkdirAll(s.opt.CacheDir, 0o700)
		s.evict()
	}

	refresh := time.NewTicker(s.opt.Refresh)
	defer refresh.Stop()
	for {
		s.index(ctx)
		select {
		case <-ctx.Done():
			return
		case <-s.reindex:
		case <-refresh.C:
		}
	}
}

// Folders lists the folders inside parent for the picker.
func (s *Service) Folders(ctx context.Context, parent string) ([]Folder, error) {
	folders, err := s.opt.Source.Folders(ctx, parent)
	if folders == nil {
		folders = []Folder{}
	}
	return folders, err
}

// SetSelection saves the user's choice of folders and re-indexes.
func (s *Service) SetSelection(sel Selection) error {
	if len(sel.Folders) > 500 {
		return errors.New("too many folders selected")
	}
	seen := map[string]bool{}
	clean := Selection{Recursive: sel.Recursive, Folders: []Folder{}}
	for _, f := range sel.Folders {
		if f.ID == "" || seen[f.ID] {
			continue
		}
		seen[f.ID] = true
		if f.Name == "" {
			f.Name = f.ID
		}
		if f.Path == "" {
			f.Path = f.Name
		}
		clean.Folders = append(clean.Folders, f)
	}
	if err := s.saveSelection(clean); err != nil {
		return err
	}
	s.mu.Lock()
	s.selection = clean
	s.mu.Unlock()
	select {
	case s.reindex <- struct{}{}:
	default:
	}
	s.notify()
	return nil
}

func (s *Service) loadSelection() (Selection, bool) {
	if s.opt.StatePath == "" {
		return Selection{}, false
	}
	data, err := os.ReadFile(s.opt.StatePath)
	if err != nil {
		return Selection{}, false
	}
	var sel Selection
	if err := json.Unmarshal(data, &sel); err != nil {
		s.opt.Log.Warn("ignoring unreadable photo selection", "path", s.opt.StatePath, "err", err)
		return Selection{}, false
	}
	return sel, true
}

func (s *Service) saveSelection(sel Selection) error {
	if s.opt.StatePath == "" {
		return nil
	}
	data, err := json.MarshalIndent(sel, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.opt.StatePath), 0o700); err != nil {
		return err
	}
	tmp := s.opt.StatePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.opt.StatePath)
}

// index reads the selected folders. If the source cannot be reached the
// previous photos are kept, so the slideshow keeps running.
func (s *Service) index(ctx context.Context) {
	s.mu.Lock()
	sel := s.selection
	s.indexing = true
	s.mu.Unlock()
	s.notify()

	start := time.Now()
	items, err := s.collect(ctx, sel)

	s.mu.Lock()
	s.indexing = false
	switch {
	case ctx.Err() != nil:
		s.mu.Unlock()
		return
	case err != nil && len(items) == 0 && len(sel.Folders) > 0:
		// Nothing could be read at all: keep what we had.
		s.lastErr = err.Error()
	default:
		s.lastErr = ""
		if err != nil {
			s.lastErr = err.Error()
		}
		if !sameItems(s.items, items) {
			s.items = items
			s.byKey = make(map[string]Item, len(items))
			for _, it := range items {
				s.byKey[it.Key] = it
			}
			s.version++
		}
	}
	count, lastErr := len(s.items), s.lastErr
	s.mu.Unlock()

	if lastErr != "" {
		s.opt.Log.Warn("photo index", "err", lastErr, "photos", count)
	} else {
		s.opt.Log.Info("photo index", "photos", count, "folders", len(sel.Folders), "took", time.Since(start).Round(time.Millisecond))
	}
	s.notify()
}

// collect walks the selected folders. It returns what it found together with
// the first error, so one unreadable folder does not hide the others.
func (s *Service) collect(ctx context.Context, sel Selection) ([]Item, error) {
	var (
		items    []Item
		firstErr error
		seenItem = map[string]bool{}
		seenDir  = map[string]bool{}
		queue    = append([]Folder{}, sel.Folders...)
	)
	for len(queue) > 0 && len(seenDir) < maxFolders && len(items) < maxItems {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		folder := queue[0]
		queue = queue[1:]
		if seenDir[folder.ID] {
			continue
		}
		seenDir[folder.ID] = true

		found, err := s.opt.Source.Items(ctx, folder)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("%s: %w", folder.Path, err)
			}
			continue
		}
		for _, it := range found {
			if it.Key == "" || seenItem[it.Key] {
				continue
			}
			seenItem[it.Key] = true
			items = append(items, it)
		}
		if sel.Recursive {
			children, err := s.opt.Source.Folders(ctx, folder.ID)
			if err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("%s: %w", folder.Path, err)
				}
				continue
			}
			queue = append(queue, children...)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Key < items[j].Key })
	return items, firstErr
}

func sameItems(a, b []Item) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Key != b[i].Key || a[i].Version != b[i].Version {
			return false
		}
	}
	return true
}

// Playlist returns up to limit photos in random order.
func (s *Service) Playlist(limit int) (version int, items []Item) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.items)
	if limit <= 0 || limit > n {
		limit = n
	}
	items = make([]Item, 0, limit)
	for _, i := range rand.Perm(n)[:limit] {
		items = append(items, s.items[i])
	}
	return s.version, items
}

// ErrNotFound is returned for a photo that is not in the selected folders.
var ErrNotFound = errors.New("photo not found")

// Image returns the path of the rendered frame for a photo, rendering and
// caching it first if needed. Only photos in the current index can be
// requested, so this cannot be used to read arbitrary files from the source.
func (s *Service) Image(ctx context.Context, key string) (string, error) {
	s.mu.Lock()
	item, ok := s.byKey[key]
	s.mu.Unlock()
	if !ok {
		return "", ErrNotFound
	}
	sum := sha1.Sum([]byte(item.Key + "|" + item.Version))
	path := filepath.Join(s.opt.CacheDir, hex.EncodeToString(sum[:12])+".jpg")

	if fi, err := os.Stat(path); err == nil && fi.Size() > 0 {
		// Mark as recently used, at most once a day, so eviction removes the
		// frames that have not been shown for the longest time.
		if time.Since(fi.ModTime()) > 24*time.Hour {
			now := time.Now()
			_ = os.Chtimes(path, now, now)
		}
		return path, nil
	}

	// Several requests for the same photo share one render.
	s.flightMu.Lock()
	if call, ok := s.flight[path]; ok {
		s.flightMu.Unlock()
		select {
		case <-call.done:
			return call.path, call.err
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	call := &flightCall{done: make(chan struct{})}
	s.flight[path] = call
	s.flightMu.Unlock()

	// Finish the render even if this particular viewer goes away.
	work, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
	call.err = s.render(work, item, path)
	cancel()
	if call.err == nil {
		call.path = path
	}
	s.flightMu.Lock()
	delete(s.flight, path)
	s.flightMu.Unlock()
	close(call.done)
	return call.path, call.err
}

func decodable(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".jpg", ".jpeg", ".png":
		return true
	}
	return false
}

func (s *Service) render(ctx context.Context, item Item, path string) error {
	src := s.opt.Source
	// Full files give the sharpest result, but only formats we can decode;
	// HEIC and RAW photos use the source's preview instead.
	useOriginal := !src.HasPreviews() || (s.opt.PreferOriginal && decodable(item.Name))

	frame, err := s.fetchAndRender(ctx, item, useOriginal)
	if err != nil && useOriginal && src.HasPreviews() {
		s.opt.Log.Debug("original not usable, using preview", "photo", item.Name, "err", err)
		frame, err = s.fetchAndRender(ctx, item, false)
	}
	if err != nil {
		return fmt.Errorf("%s: %w", item.Name, err)
	}

	tmp := path + ".part"
	if err := os.WriteFile(tmp, frame, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	s.flightMu.Lock()
	s.writes++
	evict := s.writes%50 == 0
	s.flightMu.Unlock()
	if evict {
		s.evict()
	}
	return nil
}

func (s *Service) fetchAndRender(ctx context.Context, item Item, original bool) ([]byte, error) {
	data, upright, err := s.opt.Source.Fetch(ctx, item, original)
	if err != nil {
		return nil, err
	}
	// Decoding a large photo needs a lot of memory; do one at a time.
	select {
	case s.renderSem <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-s.renderSem }()
	return Render(data, !upright)
}

// evict keeps the frame cache under its size limit, removing the frames that
// were used least recently.
func (s *Service) evict() {
	entries, err := os.ReadDir(s.opt.CacheDir)
	if err != nil {
		return
	}
	type file struct {
		path string
		size int64
		mod  time.Time
	}
	var files []file
	var total int64
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !(strings.HasSuffix(name, ".jpg") || strings.HasSuffix(name, ".part")) {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		p := filepath.Join(s.opt.CacheDir, name)
		// Leftovers of an interrupted render.
		if strings.HasSuffix(name, ".part") && time.Since(fi.ModTime()) > time.Hour {
			_ = os.Remove(p)
			continue
		}
		files = append(files, file{p, fi.Size(), fi.ModTime()})
		total += fi.Size()
	}
	if total <= s.opt.CacheBytes {
		return
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.Before(files[j].mod) })
	target := s.opt.CacheBytes * 9 / 10
	for _, f := range files {
		if total <= target {
			break
		}
		if os.Remove(f.path) == nil {
			total -= f.size
		}
	}
}
