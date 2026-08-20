// Package menus owns the primary site navigation and canonical entry links.
package menus

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/kokosx/stratumcms/internal/id"
)

var ErrValidation = errors.New("menu validation")

type Item struct {
	ID, Label, Type, EntryID, URL string
	Position                      int
}
type Service struct{ db *sql.DB }

func New(db *sql.DB) *Service { return &Service{db: db} }
func (s *Service) Primary(ctx context.Context) ([]Item, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT mi.id,mi.label,mi.item_type,COALESCE(mi.entry_id,''),CASE WHEN mi.item_type='entry' THEN COALESCE(r.path,'') ELSE COALESCE(mi.url,'') END,mi.position FROM menu_items mi LEFT JOIN routes r ON r.resource_type='entry' AND r.resource_id=mi.entry_id AND r.canonical=1 WHERE mi.menu_id='menu_primary' ORDER BY mi.position,mi.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Item
	for rows.Next() {
		var i Item
		if err := rows.Scan(&i.ID, &i.Label, &i.Type, &i.EntryID, &i.URL, &i.Position); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}
func (s *Service) Add(ctx context.Context, label, itemType, entryID, target string) error {
	label = strings.TrimSpace(label)
	if label == "" {
		return fmt.Errorf("%w: label is required", ErrValidation)
	}
	if itemType != "entry" && itemType != "custom" {
		return fmt.Errorf("%w: invalid item type", ErrValidation)
	}
	if itemType == "entry" {
		var exists int
		if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM entries WHERE id=?)`, entryID).Scan(&exists); err != nil || exists != 1 {
			return fmt.Errorf("%w: entry does not exist", ErrValidation)
		}
		target = ""
	} else {
		entryID = ""
		u, err := url.Parse(strings.TrimSpace(target))
		if err != nil || target == "" || (u.Scheme != "" && u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "mailto" && u.Scheme != "tel") {
			return fmt.Errorf("%w: enter a safe URL", ErrValidation)
		}
	}
	iid, err := id.New()
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.db.ExecContext(ctx, `INSERT INTO menu_items(id,menu_id,label,item_type,entry_id,url,position,created_at,updated_at) VALUES(?,'menu_primary',?,?,?,?,(SELECT COALESCE(MAX(position),-1)+1 FROM menu_items WHERE menu_id='menu_primary'),?,?)`, iid, label, itemType, nullString(entryID), nullString(target), now, now)
	return err
}
func (s *Service) Delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM menu_items WHERE id=? AND menu_id='menu_primary'`, id)
	return err
}
func (s *Service) Move(ctx context.Context, id string, delta int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var pos int
	if err := tx.QueryRowContext(ctx, `SELECT position FROM menu_items WHERE id=? AND menu_id='menu_primary'`, id).Scan(&pos); err != nil {
		return err
	}
	var other string
	otherPos := pos + delta
	if err := tx.QueryRowContext(ctx, `SELECT id FROM menu_items WHERE menu_id='menu_primary' AND position=?`, otherPos).Scan(&other); err != nil {
		return nil
	}
	if _, err = tx.ExecContext(ctx, `UPDATE menu_items SET position=-1 WHERE id=?`, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE menu_items SET position=? WHERE id=?`, pos, other); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE menu_items SET position=? WHERE id=?`, otherPos, id); err != nil {
		return err
	}
	return tx.Commit()
}
func nullString(v string) any {
	if v == "" {
		return nil
	}
	return v
}
