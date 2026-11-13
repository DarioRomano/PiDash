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

// How photos move on screen.
const (
	MotionOff   = "off"
	MotionWhole = "whole" // slow pans and zooms; the whole photo always stays visible
	MotionFill  = "fill"  // slow pans and zooms over a photo that fills the screen
)

// Folder is a place photos can be picked from.
type Folder struct {
	// ID identifies the folder to its source.
	ID string `json:"id"`
	// Name is the folder's own name; Path its full, human-readable location.
	Name string `json:"name"`
	Path string `json:"path"`
	// Leaf marks a folder that cannot contain others, such as an album.
	Leaf bool `json:"leaf,omitempty"`
	// Detail is a short note for the picker, e.g. "12 items". It is not saved.
	Detail string `json:"detail,omitempty"`
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
	// Motion is how photos move on screen: "off", "whole" or "fill".
	// MotionZoom is the largest zoom at which a "whole" photo still fits.
	Motion     string  `json:"motion"`
	MotionZoom float64 `json:"motionZoom"`
	// Pairs changes when more is known about which photos can be shown two
	// at a time; the dashboard then fetches its upcoming photos again.
	Pairs int `json:"pairs"`
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
	// PairPortraits shows two upright photos side by side instead of one
	// with empty space beside it.
	PairPortraits bool
	// Motion is "whole" to prepare photos for slow movement that never cuts
	// them off, "fill" for movement over photos that fill the screen, or
	// "off"/"" for photos that stand still.
	Motion string
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

	// shapes records which photos are upright, by shapeKey; pairs counts how
	// often that knowledge grew. partners is the pairing worked out from it.
	shapes      map[string]string
	pairs       int
	rescan      chan struct{}
	partners    map[string]partner
	partnersFor [2]int

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
		rescan:    make(chan struct{}, 1),
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
	s.loadShapes()
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
		Motion:          s.motion(),
		MotionZoom:      WholeZoom,
		Pairs:           s.pairs,
	}
}

func (s *Service) notify() {
	s.mu.Lock()
	st, fn := s.statusLocked(), s.onChange
	s.mu.Unlock()
	fn(st)
}

func (s *Service) motion() string {
	if s.opt.Motion == MotionWhole || s.opt.Motion == MotionFill {
		return s.opt.Motion
	}
	return MotionOff
}

// Variant names how frames are rendered. It is part of the name of every
// cached frame and of the address the dashboard loads it from, so changing
// the setting never shows a frame prepared for the other one.
func (s *Service) Variant() string {
	if s.opt.Motion == MotionWhole {
		return "whole"
	}
	return ""
}

func (s *Service) layout() Layout {
	if s.opt.Motion == MotionWhole {
		return LayoutWhole
	}
	return LayoutFill
}

// Run indexes now, whenever the selection changes, and periodically to pick up
// new photos. It returns when ctx ends.
func (s *Service) Run(ctx context.Context) {
	if s.opt.CacheDir != "" {
		_ = os.MkdirAll(s.opt.CacheDir, 0o700)
		s.evict()
	}

	if s.opt.PairPortraits {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case <-s.rescan:
					s.scanShapes(ctx)
				}
			}
		}()
	}

	refresh := time.NewTicker(s.opt.Refresh)
	defer refresh.Stop()
	for {
		s.index(ctx)
		select {
		case s.rescan <- struct{}{}:
		default:
		}
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
		f.Detail = ""
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

// pairSep joins the keys of two photos shown side by side.
const pairSep = "+"

// Slide is what is on screen at one time: a photo, or two upright photos
// side by side.
type Slide struct {
	// Key is the name in /photos/{key}.
	Key   string
	Items []Item
}

// Slides returns up to limit slides in random order. With PairPortraits,
// upright photos appear together with their partner.
func (s *Service) Slides(limit int) (version int, slides []Slide) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.items)
	if limit <= 0 || limit > n {
		limit = n
	}
	partners := s.partnersLocked()
	used := make(map[string]bool, limit)
	slides = make([]Slide, 0, limit)
	for _, i := range rand.Perm(n) {
		if len(slides) == limit {
			break
		}
		item := s.items[i]
		if used[item.Key] {
			continue
		}
		used[item.Key] = true
		if p, ok := partners[item.Key]; ok && !used[p.other.Key] {
			used[p.other.Key] = true
			left, right := item, p.other
			if !p.first {
				left, right = right, left
			}
			slides = append(slides, Slide{Key: left.Key + pairSep + right.Key, Items: []Item{left, right}})
			continue
		}
		slides = append(slides, Slide{Key: item.Key, Items: []Item{item}})
	}
	return s.version, slides
}

type partner struct {
	other Item
	// first is true when this photo stands on the left.
	first bool
}

// partnersLocked gives every upright photo that has one its partner. Pairs
// are neighbours: photos from the same folder, in the order they were taken.
// That keeps a pair from one occasion, and keeps pairs the same from one
// shuffle to the next, so a prepared frame can be used again.
func (s *Service) partnersLocked() map[string]partner {
	if !s.opt.PairPortraits {
		return nil
	}
	if s.partners != nil && s.partnersFor == [2]int{s.version, s.pairs} {
		return s.partners
	}
	var upright []Item
	for _, it := range s.items {
		if s.shapes[shapeKey(it)] == shapePortrait {
			upright = append(upright, it)
		}
	}
	sort.Slice(upright, func(i, j int) bool {
		a, b := upright[i], upright[j]
		if a.Folder != b.Folder {
			return a.Folder < b.Folder
		}
		if !a.Taken.Equal(b.Taken) {
			return a.Taken.Before(b.Taken)
		}
		return a.Key < b.Key
	})
	out := make(map[string]partner, len(upright))
	for i := 0; i+1 < len(upright); i++ {
		a, b := upright[i], upright[i+1]
		if a.Folder != b.Folder {
			continue // the last one of a folder with an odd number stays alone
		}
		out[a.Key] = partner{other: b, first: true}
		out[b.Key] = partner{other: a}
		i++
	}
	s.partners, s.partnersFor = out, [2]int{s.version, s.pairs}
	return out
}

// --- Which photos are upright ----------------------------------------------------

const (
	shapePortrait  = "p"
	shapeLandscape = "l"
)

// shapeKey names a photo in the shape table; an edited photo is looked at again.
func shapeKey(item Item) string { return item.Key + "|" + item.Version }

// Peeker is implemented by sources that can return a small image of a photo,
// enough to tell its shape, without transferring the whole file.
type Peeker interface {
	Peek(ctx context.Context, item Item) (data []byte, upright bool, err error)
}

// learnShape notes whether a photo is upright from image data that was
// fetched anyway.
func (s *Service) learnShape(item Item, data []byte, upright bool) {
	if !s.opt.PairPortraits {
		return
	}
	portrait, err := IsPortrait(data, upright)
	if err != nil {
		return
	}
	s.mu.Lock()
	s.setShapeLocked(item, portrait)
	s.mu.Unlock()
}

func (s *Service) setShapeLocked(item Item, portrait bool) {
	shape := shapeLandscape
	if portrait {
		shape = shapePortrait
	}
	s.shapes[shapeKey(item)] = shape
}

func (s *Service) shapesPath() string {
	if s.opt.CacheDir == "" {
		return ""
	}
	return filepath.Join(s.opt.CacheDir, "shapes.json")
}

func (s *Service) loadShapes() {
	s.shapes = map[string]string{}
	if p := s.shapesPath(); p != "" && s.opt.PairPortraits {
		if data, err := os.ReadFile(p); err == nil {
			_ = json.Unmarshal(data, &s.shapes)
		}
	}
}

// saveShapes stores what is known, leaving out photos that are gone.
func (s *Service) saveShapes() {
	p := s.shapesPath()
	if p == "" {
		return
	}
	s.mu.Lock()
	if len(s.items) > 0 {
		current := make(map[string]string, len(s.items))
		for _, it := range s.items {
			if shape, ok := s.shapes[shapeKey(it)]; ok {
				current[shapeKey(it)] = shape
			}
		}
		s.shapes = current
	}
	data, err := json.Marshal(s.shapes)
	s.mu.Unlock()
	if err != nil {
		return
	}
	if err := os.WriteFile(p+".tmp", data, 0o600); err == nil {
		_ = os.Rename(p+".tmp", p)
	}
}

// scanShapes looks at every photo whose shape is not known yet. It reads
// only a small image of each, so a large library takes minutes, once.
func (s *Service) scanShapes(ctx context.Context) {
	s.mu.Lock()
	var todo []Item
	for _, it := range s.items {
		if _, ok := s.shapes[shapeKey(it)]; !ok {
			todo = append(todo, it)
		}
	}
	s.mu.Unlock()
	if len(todo) == 0 {
		return
	}
	start := time.Now()
	learned, failed := 0, 0
	publish := func() {
		s.saveShapes()
		s.mu.Lock()
		s.pairs++
		s.mu.Unlock()
		s.notify()
	}
	for _, it := range todo {
		if ctx.Err() != nil {
			return
		}
		portrait, err := s.peek(ctx, it)
		if err != nil {
			// The source is probably away; the next index tries again.
			if failed++; failed >= 20 {
				break
			}
			continue
		}
		failed = 0
		s.mu.Lock()
		s.setShapeLocked(it, portrait)
		s.mu.Unlock()
		// Let the dashboard start pairing early in a large library.
		if learned++; learned%200 == 0 {
			publish()
		}
	}
	if learned%200 != 0 {
		publish()
	}
	s.opt.Log.Info("looked at photo shapes", "photos", learned, "of", len(todo), "took", time.Since(start).Round(time.Millisecond))
}

func (s *Service) peek(ctx context.Context, item Item) (bool, error) {
	if p, ok := s.opt.Source.(Peeker); ok {
		if data, upright, err := p.Peek(ctx, item); err == nil {
			if portrait, err := IsPortrait(data, upright); err == nil {
				return portrait, nil
			}
		} else if ctx.Err() != nil {
			return false, ctx.Err()
		}
	}
	data, upright, err := s.opt.Source.Fetch(ctx, item, false)
	if err != nil {
		return false, err
	}
	return IsPortrait(data, upright)
}

// ErrNotFound is returned for a photo that is not in the selected folders.
var ErrNotFound = errors.New("photo not found")

// Image returns the path of the rendered frame for a photo, rendering and
// caching it first if needed. Only photos in the current index can be
// requested, so this cannot be used to read arbitrary files from the source.
func (s *Service) Image(ctx context.Context, key string) (string, error) {
	// "a+b" asks for two photos side by side.
	keys := strings.Split(key, pairSep)
	if len(keys) > 2 || (len(keys) == 2 && keys[0] == keys[1]) {
		return "", ErrNotFound
	}
	items := make([]Item, 0, 2)
	name := ""
	s.mu.Lock()
	for _, k := range keys {
		item, ok := s.byKey[k]
		if !ok {
			s.mu.Unlock()
			return "", ErrNotFound
		}
		items = append(items, item)
		name += item.Key + "|" + item.Version + "|"
	}
	s.mu.Unlock()
	name = strings.TrimSuffix(name, "|")
	if v := s.Variant(); v != "" {
		name += "|" + v
	}
	sum := sha1.Sum([]byte(name))
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
	call.err = s.render(work, items, path)
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

// render prepares the frame for one photo, or for two side by side, and
// stores it at path.
func (s *Service) render(ctx context.Context, items []Item, path string) error {
	// Decoding a large photo needs a lot of memory; do one frame at a time.
	select {
	case s.renderSem <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-s.renderSem }()

	var frame []byte
	var err error
	if len(items) == 2 {
		pair := NewPairRenderer(s.layout())
		for _, item := range items {
			err = s.withImage(ctx, item, pair.Add)
			if err != nil {
				return fmt.Errorf("%s: %w", item.Name, err)
			}
		}
		frame, err = pair.JPEG()
	} else {
		err = s.withImage(ctx, items[0], func(data []byte, applyExif bool) error {
			var err error
			frame, err = Render(data, applyExif, s.layout())
			return err
		})
	}
	if err != nil {
		return fmt.Errorf("%s: %w", items[0].Name, err)
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

// withImage fetches a photo and hands its data to use. Full files give the
// sharpest result, but only in formats that can be decoded here; when use
// cannot work with the full file (HEIC, RAW), it is given the source's
// preview instead.
func (s *Service) withImage(ctx context.Context, item Item, use func(data []byte, applyExif bool) error) error {
	src := s.opt.Source
	useOriginal := !src.HasPreviews() || (s.opt.PreferOriginal && decodable(item.Name))
	try := func(original bool) error {
		data, upright, err := src.Fetch(ctx, item, original)
		if err != nil {
			return err
		}
		s.learnShape(item, data, upright)
		return use(data, !upright)
	}
	err := try(useOriginal)
	if err != nil && useOriginal && src.HasPreviews() && ctx.Err() == nil {
		s.opt.Log.Debug("original not usable, using preview", "photo", item.Name, "err", err)
		err = try(false)
	}
	return err
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
