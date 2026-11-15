package photos

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hirochachacha/go-smb2"
)

// Share reads photos from a shared folder on the network: a NAS, a router
// with a disk, a Windows or Mac computer (SMB, versions 2 and 3). PiDash
// reads the share itself; nothing is mounted on the Pi, and nothing is ever
// written. Folder ids are paths inside the share, "." for where it starts.
type Share struct {
	opt ShareOptions

	mu    sync.Mutex
	conn  net.Conn
	sess  *smb2.Session
	mount *smb2.Share
}

// ShareOptions say where the share is and who may read it.
type ShareOptions struct {
	// Host is the computer's name or address; a port may follow a colon.
	Host string
	// Name is the share's name, as it shows in a file manager.
	Name string
	// Folder is where the photos are inside the share; empty for all of it.
	Folder string
	// Username and Password sign in; both empty tries as a guest.
	Username string
	Password string
}

// NewShare returns a source for a shared folder.
func NewShare(o ShareOptions) *Share {
	o.Host = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(o.Host), "smb://"), "//"))
	o.Host = strings.TrimSuffix(strings.ReplaceAll(o.Host, `\`, ""), "/")
	o.Name = strings.Trim(strings.TrimSpace(o.Name), `/\`)
	o.Folder = cleanSharePath(o.Folder)
	return &Share{opt: o}
}

// cleanSharePath turns a folder as someone types it into a path inside the
// share, with nothing in it that climbs out.
func cleanSharePath(p string) string {
	p = path.Clean("/" + strings.ReplaceAll(strings.TrimSpace(p), `\`, "/"))
	return strings.TrimPrefix(p, "/")
}

// Label implements Source.
func (s *Share) Label() string {
	label := "Share //" + s.opt.Host + "/" + s.opt.Name
	if s.opt.Folder != "" {
		label += "/" + s.opt.Folder
	}
	return label
}

// HasPreviews implements Source.
func (s *Share) HasPreviews() bool { return false }

func (s *Share) rootFolder() Folder {
	name := s.opt.Name
	if s.opt.Folder != "" {
		name = path.Base(s.opt.Folder)
	}
	return Folder{ID: ".", Name: name, Path: name}
}

// DefaultSelection implements Source: everything in the share's folder.
func (s *Share) DefaultSelection() Selection {
	return Selection{Recursive: true, Folders: []Folder{s.rootFolder()}}
}

// in turns a folder id into a path inside the share, as the share wants it.
func (s *Share) in(id string) (string, error) {
	if id == "" || strings.HasPrefix(id, "/") {
		return "", fmt.Errorf("bad folder id %q", id)
	}
	clean := path.Clean(strings.ReplaceAll(id, `\`, "/"))
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("bad folder id %q", id)
	}
	full := path.Join(s.opt.Folder, clean)
	if full == "." {
		full = ""
	}
	return strings.ReplaceAll(full, "/", `\`), nil
}

// open returns the share, signing in first where that has not been done.
func (s *Share) open(ctx context.Context) (*smb2.Share, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.mount != nil {
		return s.mount, nil
	}
	if s.opt.Host == "" || s.opt.Name == "" {
		return nil, errors.New("the computer's name and the share's name are both needed")
	}
	address := s.opt.Host
	if _, _, err := net.SplitHostPort(address); err != nil {
		address = net.JoinHostPort(address, "445")
	}
	dial := net.Dialer{Timeout: 10 * time.Second}
	conn, err := dial.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("the computer %s cannot be reached: is it on, and is file sharing (SMB) switched on there?", s.opt.Host)
	}
	user, password := s.opt.Username, s.opt.Password
	if user == "" {
		user = "guest" // as file managers do where no name is given
	}
	domain := ""
	if i := strings.IndexAny(user, `\/`); i > 0 {
		domain, user = user[:i], user[i+1:]
	}
	d := &smb2.Dialer{Initiator: &smb2.NTLMInitiator{User: user, Password: password, Domain: domain}}
	sess, err := d.DialContext(ctx, conn)
	if err != nil {
		conn.Close()
		return nil, shareError(err, "signing in to "+s.opt.Host)
	}
	mount, err := sess.Mount(s.opt.Name)
	if err != nil {
		_ = sess.Logoff()
		conn.Close()
		return nil, shareError(err, "opening the share "+s.opt.Name)
	}
	s.conn, s.sess, s.mount = conn, sess, mount
	return mount, nil
}

// shareError says in words what went wrong on a share.
func shareError(err error, doing string) error {
	text := strings.ToLower(err.Error())
	switch {
	case errors.Is(err, fs.ErrPermission) || strings.Contains(text, "logon") || strings.Contains(text, "password") ||
		strings.Contains(text, "denied") || strings.Contains(text, "account"):
		return fmt.Errorf("%s: the user name or password is not accepted, or this user may not read the share", doing)
	case errors.Is(err, fs.ErrNotExist) || strings.Contains(text, "bad network name") || strings.Contains(text, "not found") ||
		strings.Contains(text, "no such") || strings.Contains(text, "does not exist"):
		return fmt.Errorf("%s: there is no such share or folder", doing)
	}
	return fmt.Errorf("%s: %w", doing, err)
}

// drop forgets the connection, so that the next request makes a new one.
func (s *Share) drop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.mount != nil {
		_ = s.mount.Umount()
	}
	if s.sess != nil {
		_ = s.sess.Logoff()
	}
	if s.conn != nil {
		s.conn.Close()
	}
	s.conn, s.sess, s.mount = nil, nil, nil
}

// with does something on the share. A connection that has gone stale (the
// NAS slept, the network blinked) is made anew once.
func (s *Share) with(ctx context.Context, do func(*smb2.Share) error) error {
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		var mount *smb2.Share
		if mount, err = s.open(ctx); err != nil {
			return err
		}
		if err = do(mount.WithContext(ctx)); err == nil || errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) || ctx.Err() != nil {
			return err
		}
		s.drop()
	}
	return err
}

// Check signs in and looks into the folder: for the settings page.
func (s *Share) Check(ctx context.Context) (int, error) {
	defer s.drop()
	n := 0
	dir, _ := s.in(".")
	err := s.with(ctx, func(m *smb2.Share) error {
		entries, err := m.ReadDir(dir)
		if err != nil {
			return shareError(err, "reading the folder")
		}
		for _, e := range entries {
			if !hidden(e.Name()) {
				n++
			}
		}
		return nil
	})
	return n, err
}

// Folders implements Source.
func (s *Share) Folders(ctx context.Context, parent string) ([]Folder, error) {
	if parent == "" {
		dir, _ := s.in(".")
		if err := s.with(ctx, func(m *smb2.Share) error { _, err := m.ReadDir(dir); return err }); err != nil {
			return nil, err
		}
		return []Folder{s.rootFolder()}, nil
	}
	dir, err := s.in(parent)
	if err != nil {
		return nil, err
	}
	var out []Folder
	err = s.with(ctx, func(m *smb2.Share) error {
		entries, err := m.ReadDir(dir)
		if err != nil {
			return err
		}
		out = out[:0]
		for _, e := range entries {
			if hidden(e.Name()) || !e.IsDir() {
				continue
			}
			rel := path.Join(path.Clean(parent), e.Name())
			out = append(out, Folder{ID: rel, Name: e.Name(), Path: s.rootFolder().Name + " / " + strings.ReplaceAll(rel, "/", " / ")})
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, err
}

// Items implements Source.
func (s *Share) Items(ctx context.Context, folder Folder) ([]Item, error) {
	dir, err := s.in(folder.ID)
	if err != nil {
		return nil, err
	}
	var out []Item
	err = s.with(ctx, func(m *smb2.Share) error {
		entries, err := m.ReadDir(dir)
		if err != nil {
			return err
		}
		out = out[:0]
		for _, e := range entries {
			if hidden(e.Name()) || !decodable(e.Name()) || !e.Mode().IsRegular() {
				continue
			}
			rel := path.Join(path.Clean(folder.ID), e.Name())
			sum := sha1.Sum([]byte(rel))
			out = append(out, Item{
				Key:     "s-" + hex.EncodeToString(sum[:10]),
				Name:    e.Name(),
				Folder:  folder.Path,
				Taken:   e.ModTime().UTC(),
				Version: fmt.Sprintf("%d-%d", e.ModTime().Unix(), e.Size()),
				Size:    e.Size(),
				ID:      rel,
			})
		}
		return nil
	})
	return out, err
}

// read returns the start of a file, or with most <= 0 all of it.
func (s *Share) read(ctx context.Context, item Item, most int64) ([]byte, error) {
	name, err := s.in(item.ID)
	if err != nil {
		return nil, err
	}
	var data []byte
	err = s.with(ctx, func(m *smb2.Share) error {
		f, err := m.Open(name)
		if err != nil {
			return err
		}
		defer f.Close()
		if most <= 0 {
			fi, err := f.Stat()
			if err != nil {
				return err
			}
			if fi.Size() > 100<<20 {
				return errors.New("file is too large")
			}
			most = fi.Size()
		}
		data, err = io.ReadAll(io.LimitReader(f, most))
		return err
	})
	return data, err
}

// Fetch implements Source.
func (s *Share) Fetch(ctx context.Context, item Item, _ bool) ([]byte, bool, error) {
	data, err := s.read(ctx, item, 0)
	return data, false, err
}

// Peek implements Peeker: the start of the file holds its size and rotation.
func (s *Share) Peek(ctx context.Context, item Item) ([]byte, bool, error) {
	data, err := s.read(ctx, item, 256<<10)
	return data, false, err
}

// Details implements Detailer: the camera wrote the time and, if it knew,
// the place into the start of the file.
func (s *Share) Details(ctx context.Context, item Item) (Extra, error) {
	data, _, err := s.Peek(ctx, item)
	if err != nil {
		return Extra{}, err
	}
	return readExtra(data), nil
}
