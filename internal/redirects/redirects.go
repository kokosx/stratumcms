// Package redirects owns validated local redirect rules.
package redirects

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/kokosx/stratumcms/internal/id"
	"net/url"
	"path"
	"strings"
	"time"
)

var ErrValidation = errors.New("redirect validation")

type Rule struct {
	ID, FromPath, ToPath, CreatedAt string
	StatusCode                      int
}
type Service struct {
	db  *sql.DB
	now func() time.Time
}

func New(db *sql.DB) *Service { return &Service{db: db, now: time.Now} }
func Validate(from, to string, status int) error {
	if status == 0 {
		status = 301
	}
	if status != 301 && status != 308 {
		return fmt.Errorf("%w: status must be 301 or 308", ErrValidation)
	}
	for _, p := range []string{from, to} {
		if !strings.HasPrefix(p, "/") || strings.ContainsAny(p, "\\\r\n\t") {
			return fmt.Errorf("%w: paths must be local absolute paths", ErrValidation)
		}
		u, e := url.Parse(p)
		if e != nil || u.IsAbs() || u.Host != "" {
			return fmt.Errorf("%w: paths must be local", ErrValidation)
		}
		for _, prefix := range []string{"/admin", "/static", "/assets", "/media", "/setup", "/login"} {
			if p == prefix || strings.HasPrefix(p, prefix+"/") {
				return fmt.Errorf("%w: reserved path", ErrValidation)
			}
		}
	}
	if from == to {
		return fmt.Errorf("%w: source and destination match", ErrValidation)
	}
	return nil
}
func (s *Service) Get(ctx context.Context, from string) (Rule, error) {
	var r Rule
	err := s.db.QueryRowContext(ctx, `SELECT id,from_path,to_path,status_code,created_at FROM redirects WHERE from_path=?`, from).Scan(&r.ID, &r.FromPath, &r.ToPath, &r.StatusCode, &r.CreatedAt)
	return r, err
}
func (s *Service) Create(ctx context.Context, from, to string, status int) (Rule, error) {
	if status == 0 {
		status = 301
	}
	if err := Validate(from, to, status); err != nil {
		return Rule{}, err
	}
	from, pathTo := path.Clean(from), path.Clean(to)
	var shadow int
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM routes WHERE path=? AND canonical=1)`, from).Scan(&shadow); err != nil {
		return Rule{}, err
	}
	if shadow != 0 {
		return Rule{}, fmt.Errorf("%w: source shadows a published route", ErrValidation)
	}
	seen := map[string]bool{from: true}
	next := pathTo
	for i := 0; i < 32; i++ {
		if seen[next] {
			return Rule{}, fmt.Errorf("%w: redirect loop", ErrValidation)
		}
		seen[next] = true
		var candidate string
		err := s.db.QueryRowContext(ctx, `SELECT to_path FROM redirects WHERE from_path=?`, next).Scan(&candidate)
		if errors.Is(err, sql.ErrNoRows) {
			break
		}
		if err != nil {
			return Rule{}, err
		}
		next = candidate
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	rid, err := id.New()
	if err != nil {
		return Rule{}, err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO redirects (id,from_path,to_path,status_code,created_at,updated_at) VALUES (?,?,?,?,?,?)`, rid, from, pathTo, status, now, now)
	if err != nil {
		return Rule{}, err
	}
	return Rule{rid, from, pathTo, now, status}, nil
}
func (s *Service) List(ctx context.Context) ([]Rule, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,from_path,to_path,status_code,created_at FROM redirects ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Rule
	for rows.Next() {
		var r Rule
		if err := rows.Scan(&r.ID, &r.FromPath, &r.ToPath, &r.StatusCode, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (s *Service) Delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM redirects WHERE id=?`, id)
	return err
}
