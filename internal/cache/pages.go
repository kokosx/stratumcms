// Package cache provides rebuildable filesystem-backed public page caching.
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const maxHTMLSize = 4 << 20

type Entry struct {
	Path         string    `json:"path"`
	HTML         []byte    `json:"-"`
	ETag         string    `json:"etag"`
	HTMLHash     string    `json:"html_hash"`
	CreatedAt    time.Time `json:"created_at"`
	Dependencies []string  `json:"dependencies"`
}

type Pages struct {
	dir string
	mu  sync.Mutex
}

func New(dataDir string) (*Pages, error) {
	dir := filepath.Join(dataDir, "cache", "pages")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create page cache: %w", err)
	}
	return &Pages{dir: dir}, nil
}

func normalize(p string) (string, error) {
	if p == "" || !strings.HasPrefix(p, "/") || strings.Contains(p, "\\") {
		return "", errors.New("invalid cache path")
	}
	n := path.Clean(p)
	if n == "." || !strings.HasPrefix(n, "/") || strings.Contains(n, "..") {
		return "", errors.New("invalid cache path")
	}
	return n, nil
}
func (p *Pages) names(requestPath string) (string, string, string, error) {
	n, err := normalize(requestPath)
	if err != nil {
		return "", "", "", err
	}
	h := sha256.Sum256([]byte(n))
	base := filepath.Join(p.dir, hex.EncodeToString(h[:]))
	return n, base + ".html", base + ".json", nil
}
func (p *Pages) Get(requestPath string) (Entry, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	n, htmlName, metaName, err := p.names(requestPath)
	if err != nil {
		return Entry{}, false
	}
	meta, err := os.ReadFile(metaName)
	if err != nil {
		return Entry{}, false
	}
	var entry Entry
	if err := json.Unmarshal(meta, &entry); err != nil || entry.Path != n || entry.ETag == "" || entry.HTMLHash == "" {
		_ = os.Remove(metaName)
		_ = os.Remove(htmlName)
		return Entry{}, false
	}
	html, err := os.ReadFile(htmlName)
	if err != nil || len(html) > maxHTMLSize {
		_ = os.Remove(metaName)
		_ = os.Remove(htmlName)
		return Entry{}, false
	}
	hash := sha256.Sum256(html)
	if entry.HTMLHash != hex.EncodeToString(hash[:]) {
		_ = os.Remove(metaName)
		_ = os.Remove(htmlName)
		return Entry{}, false
	}
	entry.HTML = html
	return entry, true
}
func (p *Pages) Put(entry Entry) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	n, htmlName, metaName, err := p.names(entry.Path)
	if err != nil {
		return err
	}
	if len(entry.HTML) == 0 || len(entry.HTML) > maxHTMLSize {
		return errors.New("invalid cached HTML size")
	}
	entry.Path = n
	hash := sha256.Sum256(entry.HTML)
	entry.HTMLHash = hex.EncodeToString(hash[:])
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	meta, err := json.Marshal(struct {
		Path         string    `json:"path"`
		ETag         string    `json:"etag"`
		HTMLHash     string    `json:"html_hash"`
		CreatedAt    time.Time `json:"created_at"`
		Dependencies []string  `json:"dependencies"`
	}{entry.Path, entry.ETag, entry.HTMLHash, entry.CreatedAt, entry.Dependencies})
	if err != nil {
		return fmt.Errorf("marshal cache metadata: %w", err)
	}
	if err := atomicWrite(htmlName, entry.HTML); err != nil {
		return err
	}
	if err := atomicWrite(metaName, meta); err != nil {
		_ = os.Remove(htmlName)
		return err
	}
	return nil
}
func atomicWrite(name string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(name), ".page-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write cache artifact: %w", err)
	}
	if err := os.Rename(tmp, name); err != nil {
		return fmt.Errorf("replace cache artifact: %w", err)
	}
	return nil
}
func (p *Pages) InvalidatePath(requestPath string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.invalidatePath(requestPath)
}
func (p *Pages) invalidatePath(requestPath string) error {
	_, htmlName, metaName, err := p.names(requestPath)
	if err != nil {
		return err
	}
	for _, name := range []string{htmlName, metaName} {
		if err := os.Remove(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}
func (p *Pages) InvalidateTag(tag string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	files, err := filepath.Glob(filepath.Join(p.dir, "*.json"))
	if err != nil {
		return err
	}
	for _, name := range files {
		data, err := os.ReadFile(name)
		if err != nil {
			continue
		}
		var entry Entry
		if json.Unmarshal(data, &entry) != nil {
			_ = os.Remove(name)
			_ = os.Remove(strings.TrimSuffix(name, ".json") + ".html")
			continue
		}
		for _, dep := range entry.Dependencies {
			if dep == tag {
				if err := p.invalidatePath(entry.Path); err != nil {
					return err
				}
				break
			}
		}
	}
	return nil
}
func (p *Pages) Clear() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	files, err := filepath.Glob(filepath.Join(p.dir, "*"))
	if err != nil {
		return err
	}
	for _, name := range files {
		if err := os.Remove(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}
