// Package media owns image metadata and safe filesystem-backed image storage.
package media

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kokosx/stratumcms/internal/id"
)

const MaxUploadSize = 20 << 20

var (
	ErrNotFound   = errors.New("media not found")
	ErrValidation = errors.New("media validation")
	ErrInUse      = errors.New("media is in use")
)

type Item struct {
	ID, StorageKey, OriginalName, MIMEType, AltText, Caption, CreatedBy, CreatedAt, UpdatedAt string
	Size                                                                                      int64
	Width, Height                                                                             int
}

func (i Item) PublicURL() string { return "/media/" + i.ID + "/" + filepath.Base(i.StorageKey) }

type Service struct {
	db  *sql.DB
	dir string
	now func() time.Time
}

func New(db *sql.DB, dataDir string) (*Service, error) {
	dir := filepath.Join(dataDir, "media")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create media storage: %w", err)
	}
	return &Service{db: db, dir: dir, now: time.Now}, nil
}
func (s *Service) Upload(ctx context.Context, createdBy, originalName string, source io.Reader) (Item, error) {
	tmp, err := os.CreateTemp(s.dir, ".upload-*")
	if err != nil {
		return Item{}, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	limited := io.LimitReader(source, MaxUploadSize+1)
	size, err := io.Copy(tmp, limited)
	if err != nil {
		tmp.Close()
		return Item{}, err
	}
	if size > MaxUploadSize {
		tmp.Close()
		return Item{}, fmt.Errorf("%w: file exceeds 20 MiB", ErrValidation)
	}
	if _, err = tmp.Seek(0, io.SeekStart); err != nil {
		tmp.Close()
		return Item{}, err
	}
	head := make([]byte, 512)
	n, _ := io.ReadFull(tmp, head)
	mime := http.DetectContentType(head[:n])
	if mime != "image/jpeg" && mime != "image/png" && mime != "image/gif" {
		tmp.Close()
		return Item{}, fmt.Errorf("%w: only JPEG, PNG, and GIF images are supported", ErrValidation)
	}
	if _, err = tmp.Seek(0, io.SeekStart); err != nil {
		tmp.Close()
		return Item{}, err
	}
	cfg, _, err := image.DecodeConfig(tmp)
	if err != nil || cfg.Width < 1 || cfg.Height < 1 {
		tmp.Close()
		return Item{}, fmt.Errorf("%w: invalid image", ErrValidation)
	}
	if err = tmp.Close(); err != nil {
		return Item{}, err
	}
	mediaID, err := id.New()
	if err != nil {
		return Item{}, err
	}
	ext := map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/gif": ".gif"}[mime]
	key := filepath.ToSlash(filepath.Join(mediaID, "original"+ext))
	final := filepath.Join(s.dir, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(final), 0o755); err != nil {
		return Item{}, err
	}
	if err := os.Rename(tmpName, final); err != nil {
		return Item{}, fmt.Errorf("store uploaded image: %w", err)
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	item := Item{ID: mediaID, StorageKey: key, OriginalName: filepath.Base(originalName), MIMEType: mime, Size: size, Width: cfg.Width, Height: cfg.Height, CreatedBy: createdBy, CreatedAt: now, UpdatedAt: now}
	_, err = s.db.ExecContext(ctx, `INSERT INTO media (id,storage_key,original_name,mime_type,size,width,height,alt_text,caption,created_by,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`, item.ID, item.StorageKey, item.OriginalName, item.MIMEType, item.Size, item.Width, item.Height, item.AltText, item.Caption, item.CreatedBy, item.CreatedAt, item.UpdatedAt)
	if err != nil {
		_ = os.Remove(final)
		return Item{}, fmt.Errorf("save media metadata: %w", err)
	}
	return item, nil
}
func (s *Service) Get(ctx context.Context, mediaID string) (Item, error) {
	var i Item
	err := s.db.QueryRowContext(ctx, `SELECT id,storage_key,original_name,mime_type,size,width,height,alt_text,caption,created_by,created_at,updated_at FROM media WHERE id=?`, mediaID).Scan(&i.ID, &i.StorageKey, &i.OriginalName, &i.MIMEType, &i.Size, &i.Width, &i.Height, &i.AltText, &i.Caption, &i.CreatedBy, &i.CreatedAt, &i.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Item{}, ErrNotFound
	}
	if err != nil {
		return Item{}, err
	}
	if !safeStorageKey(i.ID, i.StorageKey) {
		return Item{}, fmt.Errorf("invalid media storage key")
	}
	return i, nil
}
func (s *Service) List(ctx context.Context) ([]Item, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,storage_key,original_name,mime_type,size,width,height,alt_text,caption,created_by,created_at,updated_at FROM media ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Item
	for rows.Next() {
		var i Item
		if err = rows.Scan(&i.ID, &i.StorageKey, &i.OriginalName, &i.MIMEType, &i.Size, &i.Width, &i.Height, &i.AltText, &i.Caption, &i.CreatedBy, &i.CreatedAt, &i.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}
func (s *Service) UpdateMetadata(ctx context.Context, mediaID, alt, caption string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE media SET alt_text=?,caption=?,updated_at=? WHERE id=?`, alt, caption, s.now().UTC().Format(time.RFC3339Nano), mediaID)
	return err
}
func (s *Service) File(i Item) string { return filepath.Join(s.dir, filepath.FromSlash(i.StorageKey)) }
func (s *Service) PublicURL(i Item) string {
	return i.PublicURL()
}

// ResolveMedia is the narrow rendering dependency; it never exposes filesystem paths.
func (s *Service) ResolveMedia(mediaID string) (string, string, error) {
	i, err := s.Get(context.Background(), mediaID)
	if err != nil {
		return "", "", err
	}
	return s.PublicURL(i), i.AltText, nil
}
func (s *Service) Delete(ctx context.Context, mediaID string) error {
	i, err := s.Get(ctx, mediaID)
	if err != nil {
		return err
	}
	inUse, err := s.FindUsage(ctx, mediaID)
	if err != nil {
		return err
	}
	if inUse {
		return ErrInUse
	}
	file := s.File(i)
	quarantine := file + ".deleting"
	if err = os.Rename(file, quarantine); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if _, err = s.db.ExecContext(ctx, `DELETE FROM media WHERE id=?`, mediaID); err != nil {
		if quarantine != file {
			_ = os.Rename(quarantine, file)
		}
		return err
	}
	_ = os.Remove(quarantine)
	_ = os.Remove(filepath.Dir(file))
	return nil
}

// FindUsage is intentionally simple for the MVP: documents are small JSON snapshots.
func (s *Service) FindUsage(ctx context.Context, mediaID string) (bool, error) {
	needle := `"media":"` + mediaID + `"`
	var found int
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM revisions WHERE instr(document_json, ?) > 0 UNION SELECT 1 FROM entry_drafts WHERE instr(document_json, ?) > 0)`, needle, needle).Scan(&found)
	return found != 0, err
}
func SafePublicID(value string) bool { return value != "" && !strings.ContainsAny(value, "/\\") }
func safeStorageKey(id, key string) bool {
	return SafePublicID(id) && !strings.Contains(key, "\\") && filepath.ToSlash(filepath.Clean(filepath.FromSlash(key))) == key && strings.HasPrefix(key, id+"/")
}
