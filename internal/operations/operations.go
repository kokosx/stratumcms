// Package operations implements read-only diagnostics and portable site operations.
package operations

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/kokosx/stratumcms/internal/config"
	"github.com/kokosx/stratumcms/internal/documents"
	"github.com/kokosx/stratumcms/internal/migrations"
	"github.com/kokosx/stratumcms/internal/storage/turso"
	"github.com/kokosx/stratumcms/internal/styles"
	"github.com/kokosx/stratumcms/internal/themes"
)

const FormatVersion = 1

type ManifestFile struct {
	Path, SHA256 string
	Size         int64
}
type Manifest struct {
	FormatVersion             int `json:"format_version"`
	CreatedAt, StratumVersion string
	SchemaVersion             int            `json:"schema_version"`
	Files                     []ManifestFile `json:"files"`
}

func Doctor(ctx context.Context, cfg config.Config, out io.Writer) error {
	fail := false
	line := func(status, name, detail string) {
		if detail != "" {
			fmt.Fprintf(out, "%s %s: %s\n", status, name, detail)
		} else {
			fmt.Fprintf(out, "%s %s\n", status, name)
		}
		if status == "FAIL" {
			fail = true
		}
	}
	info, err := os.Stat(cfg.DataDir)
	if err != nil || !info.IsDir() {
		line("FAIL", "data directory", "not accessible")
		return errors.New("doctor found failures")
	}
	line("OK", "data directory", "")
	dbPath := filepath.Join(cfg.DataDir, "stratum.db")
	if _, err := os.Stat(dbPath); err != nil {
		line("FAIL", "database", "stratum.db is missing")
		return errors.New("doctor found failures")
	}
	db, err := turso.Open(ctx, cfg.DataDir)
	if err != nil {
		line("FAIL", "database", "cannot open")
		return errors.New("doctor found failures")
	}
	defer db.Close()
	line("OK", "database", "")
	var fk int
	if err := db.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk); err != nil || fk != 1 {
		line("FAIL", "foreign keys", "disabled")
	} else {
		line("OK", "foreign keys", "")
	}
	var current int
	if err := db.QueryRowContext(ctx, "SELECT COALESCE(MAX(version),0) FROM schema_migrations").Scan(&current); err != nil || current != migrations.LatestVersion() {
		line("FAIL", "migrations", fmt.Sprintf("schema=%d required=%d", current, migrations.LatestVersion()))
	} else {
		line("OK", "migrations", fmt.Sprintf("version %d", current))
	}
	var integrity string
	if err := db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		line("FAIL", "integrity check", "failed")
	} else {
		line("OK", "integrity check", "")
	}
	registry, err := themes.NewRegistry()
	if err != nil {
		line("FAIL", "themes", "embedded themes invalid")
	} else {
		var active, raw string
		if err := db.QueryRowContext(ctx, "SELECT active_theme,styles_json FROM site_presentation WHERE id=1").Scan(&active, &raw); err != nil {
			line("FAIL", "site presentation", "unreadable")
		} else {
			if _, ok := registry.Resolve(active); !ok {
				line("FAIL", "theme", "active theme is unavailable")
			} else {
				line("OK", "theme", active)
			}
			t := styles.Defaults()
			if raw != "{}" && json.Unmarshal([]byte(raw), &t) != nil {
				line("FAIL", "site styles", "invalid JSON")
			} else if err := styles.Validate(t); err != nil {
				line("FAIL", "site styles", "invalid tokens")
			} else {
				line("OK", "site styles", "")
			}
		}
	}
	rows, err := db.QueryContext(ctx, "SELECT id,document_json FROM revisions WHERE id IN (SELECT published_revision_id FROM entries WHERE published_revision_id IS NOT NULL)")
	if err != nil {
		line("FAIL", "published documents", "unreadable")
	} else {
		defer rows.Close()
		bad := 0
		for rows.Next() {
			var id, raw string
			_ = rows.Scan(&id, &raw)
			if _, err := documents.Parse([]byte(raw)); err != nil {
				bad++
			}
		}
		if bad > 0 {
			line("FAIL", "published documents", fmt.Sprintf("%d invalid", bad))
		} else {
			line("OK", "published documents", "")
		}
	}
	for _, dir := range []string{"media", "cache/pages", "tmp"} {
		p := filepath.Join(cfg.DataDir, dir)
		if st, err := os.Stat(p); err != nil || !st.IsDir() {
			line("WARN", dir, "directory is missing")
		} else if st.Mode().Perm()&0200 == 0 {
			line("FAIL", dir, "not writable")
		} else {
			line("OK", dir, "")
		}
	}
	if !cfg.SecureCookies && !isLoopbackAddr(cfg.Addr) {
		line("WARN", "secure cookies", "disabled on a non-loopback bind")
	}
	if fail {
		return errors.New("doctor found failures")
	}
	return nil
}

func isLoopbackAddr(addr string) bool {
	host, _, err := strings.Cut(addr, ":")
	if !err {
		return false
	}
	host = strings.Trim(host, "[]")
	return host == "" || host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func Maintenance(ctx context.Context, cfg config.Config, out io.Writer) error {
	db, err := turso.Open(ctx, cfg.DataDir)
	if err != nil {
		return err
	}
	defer db.Close()
	res, err := db.ExecContext(ctx, "DELETE FROM sessions WHERE expires_at <= ?", time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("purge sessions: %w", err)
	}
	sessions, _ := res.RowsAffected()
	removed := 0
	cutoff := time.Now().Add(-24 * time.Hour)
	for _, root := range []string{filepath.Join(cfg.DataDir, "tmp"), filepath.Join(cfg.DataDir, "cache", "pages")} {
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, e error) error {
			if e != nil || d.IsDir() {
				return nil
			}
			info, e := d.Info()
			if e == nil && info.ModTime().Before(cutoff) && (root != filepath.Join(cfg.DataDir, "cache", "pages") || strings.HasPrefix(d.Name(), ".page-")) {
				if os.Remove(p) == nil {
					removed++
				}
			}
			return nil
		})
	}
	fmt.Fprintf(out, "OK maintenance: removed %d expired sessions and %d temporary files\n", sessions, removed)
	return nil
}

type archiveSource struct {
	archivePath, diskPath string
	size                  int64
	hash                  string
}

func Backup(ctx context.Context, cfg config.Config, version, output string) (string, error) {
	if _, err := os.Stat(filepath.Join(cfg.DataDir, "stratum.db")); err != nil {
		return "", fmt.Errorf("database not found: %w", err)
	}
	db, err := turso.Open(ctx, cfg.DataDir)
	if err != nil {
		return "", err
	}
	defer db.Close()
	tmp, err := os.CreateTemp("", "stratum-snapshot-*.db")
	if err != nil {
		return "", err
	}
	snapshot := tmp.Name()
	tmp.Close()
	os.Remove(snapshot)
	defer os.Remove(snapshot)
	statement := "VACUUM INTO '" + strings.ReplaceAll(snapshot, "'", "''") + "'"
	if _, err = db.ExecContext(ctx, statement); err != nil {
		return "", fmt.Errorf("create consistent database snapshot: %w", err)
	}
	var schema int
	if err := db.QueryRowContext(ctx, "SELECT COALESCE(MAX(version),0) FROM schema_migrations").Scan(&schema); err != nil {
		return "", err
	}
	sources := []archiveSource{{"stratum.db", snapshot, 0, ""}}
	for _, dir := range []string{"media", "themes", "blocks"} {
		root := filepath.Join(cfg.DataDir, dir)
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if d.IsDir() {
				return nil
			}
			info, e := d.Info()
			if e != nil {
				return e
			}
			if info.Mode().IsRegular() {
				rel, _ := filepath.Rel(cfg.DataDir, p)
				sources = append(sources, archiveSource{filepath.ToSlash(rel), p, 0, ""})
			}
			return nil
		})
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].archivePath < sources[j].archivePath })
	manifest := Manifest{FormatVersion: FormatVersion, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), StratumVersion: version, SchemaVersion: schema}
	for i := range sources {
		f, e := os.Open(sources[i].diskPath)
		if e != nil {
			return "", e
		}
		h := sha256.New()
		n, e := io.Copy(h, f)
		f.Close()
		if e != nil {
			return "", e
		}
		sources[i].size = n
		sources[i].hash = hex.EncodeToString(h.Sum(nil))
		manifest.Files = append(manifest.Files, ManifestFile{sources[i].archivePath, sources[i].hash, n})
	}
	if output == "" {
		if err := os.MkdirAll(filepath.Join(cfg.DataDir, "backups"), 0755); err != nil {
			return "", err
		}
		output = filepath.Join(cfg.DataDir, "backups", "stratum-"+time.Now().UTC().Format("20060102T150405Z")+".tar.gz")
	}
	if err := writeArchive(output, sources, manifest); err != nil {
		return "", err
	}
	if _, err := ReadManifest(output); err != nil {
		os.Remove(output)
		return "", fmt.Errorf("verify backup: %w", err)
	}
	return output, nil
}

func writeArchive(output string, sources []archiveSource, manifest Manifest) error {
	if err := os.MkdirAll(filepath.Dir(output), 0755); err != nil {
		return err
	}
	f, err := os.Create(output)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		f.Close()
		if !ok {
			os.Remove(output)
		}
	}()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, s := range sources {
		if err := tw.WriteHeader(&tar.Header{Name: s.archivePath, Mode: 0600, Size: s.size, ModTime: time.Now()}); err != nil {
			return err
		}
		in, err := os.Open(s.diskPath)
		if err != nil {
			return err
		}
		_, err = io.Copy(tw, in)
		in.Close()
		if err != nil {
			return err
		}
	}
	data, _ := json.MarshalIndent(manifest, "", "  ")
	if err := tw.WriteHeader(&tar.Header{Name: "manifest.json", Mode: 0600, Size: int64(len(data)), ModTime: time.Now()}); err != nil {
		return err
	}
	if _, err := tw.Write(data); err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	ok = true
	return f.Close()
}

func ReadManifest(archive string) (Manifest, error) {
	files, manifest, err := readArchive(archive, "")
	if err != nil {
		return Manifest{}, err
	}
	if len(files) != len(manifest.Files) {
		return Manifest{}, errors.New("manifest file count mismatch")
	}
	return manifest, nil
}

func Restore(ctx context.Context, cfg config.Config, archive string) error {
	entries, err := os.ReadDir(cfg.DataDir)
	if err == nil && len(entries) > 0 {
		return errors.New("target data directory is not empty")
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	tmp, err := os.MkdirTemp("", "stratum-restore-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	_, manifest, err := readArchive(archive, tmp)
	if err != nil {
		return err
	}
	if manifest.FormatVersion != FormatVersion {
		return fmt.Errorf("unsupported backup format %d", manifest.FormatVersion)
	}
	if err := os.MkdirAll(cfg.DataDir, 0755); err != nil {
		return err
	}
	for _, file := range manifest.Files {
		src := filepath.Join(tmp, filepath.FromSlash(file.Path))
		dst := filepath.Join(cfg.DataDir, filepath.FromSlash(file.Path))
		if err := copyFile(src, dst); err != nil {
			return err
		}
	}
	for _, d := range []string{"cache/pages", "tmp", "backups"} {
		if err := os.MkdirAll(filepath.Join(cfg.DataDir, d), 0755); err != nil {
			return err
		}
	}
	db, err := turso.Open(ctx, cfg.DataDir)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := migrations.Run(ctx, db); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, "DELETE FROM sessions"); err != nil {
		return err
	}
	var integrity string
	if err := db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		return errors.New("restored database integrity check failed")
	}
	return nil
}

func readArchive(archive, target string) (map[string]string, Manifest, error) {
	f, err := os.Open(archive)
	if err != nil {
		return nil, Manifest{}, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, Manifest{}, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	hashes := map[string]string{}
	var manifest Manifest
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, Manifest{}, err
		}
		name := path.Clean(h.Name)
		if name == "." || strings.HasPrefix(name, "/") || strings.HasPrefix(name, "../") || strings.Contains(name, "\\") {
			return nil, Manifest{}, fmt.Errorf("unsafe archive path %q", h.Name)
		}
		if h.Typeflag != tar.TypeReg {
			return nil, Manifest{}, fmt.Errorf("unsupported archive entry %q", name)
		}
		if h.Size < 0 || h.Size > 1<<34 {
			return nil, Manifest{}, fmt.Errorf("invalid archive entry size")
		}
		if name == "manifest.json" {
			if h.Size > 1<<20 {
				return nil, Manifest{}, errors.New("manifest too large")
			}
			if err := json.NewDecoder(io.LimitReader(tr, h.Size)).Decode(&manifest); err != nil {
				return nil, Manifest{}, err
			}
			continue
		}
		hasher := sha256.New()
		var writer io.Writer = hasher
		if target != "" {
			dst := filepath.Join(target, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
				return nil, Manifest{}, err
			}
			out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err != nil {
				return nil, Manifest{}, err
			}
			writer = io.MultiWriter(hasher, out)
			_, err = io.CopyN(writer, tr, h.Size)
			closeErr := out.Close()
			if err != nil {
				return nil, Manifest{}, err
			}
			if closeErr != nil {
				return nil, Manifest{}, closeErr
			}
		} else {
			if _, err := io.CopyN(writer, tr, h.Size); err != nil {
				return nil, Manifest{}, err
			}
		}
		hashes[name] = hex.EncodeToString(hasher.Sum(nil))
	}
	if manifest.FormatVersion == 0 {
		return nil, Manifest{}, errors.New("manifest missing")
	}
	expected := map[string]ManifestFile{}
	for _, file := range manifest.Files {
		if _, ok := expected[file.Path]; ok {
			return nil, Manifest{}, errors.New("duplicate manifest path")
		}
		expected[file.Path] = file
	}
	if len(expected) != len(hashes) {
		return nil, Manifest{}, errors.New("archive contents do not match manifest")
	}
	for name, hash := range hashes {
		file, ok := expected[name]
		if !ok || file.SHA256 != hash {
			return nil, Manifest{}, fmt.Errorf("checksum mismatch for %s", name)
		}
	}
	return hashes, manifest, nil
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

var _ = sql.ErrNoRows
