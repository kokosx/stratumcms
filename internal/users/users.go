// Package users owns administrator-managed user accounts and session invalidation.
package users

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kokosx/stratumcms/internal/auth"
	"github.com/kokosx/stratumcms/internal/id"
)

var ErrValidation = errors.New("user validation")

type User struct{ ID, Email, Username, DisplayName, Role, CreatedAt string }
type Input struct{ Email, Username, DisplayName, Role, Password string }
type Service struct{ db *sql.DB }

func New(db *sql.DB) *Service { return &Service{db: db} }

func (s *Service) List(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,email,username,display_name,role,created_at FROM users ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Email, &u.Username, &u.DisplayName, &u.Role, &u.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func validate(in Input, passwordRequired bool) (Input, error) {
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	in.Username = strings.ToLower(strings.TrimSpace(in.Username))
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	if !strings.Contains(in.Email, "@") {
		return in, fmt.Errorf("%w: enter a valid email address", ErrValidation)
	}
	if len(in.Username) < 3 || strings.Contains(in.Username, "@") {
		return in, fmt.Errorf("%w: username must be at least 3 characters and cannot contain @", ErrValidation)
	}
	if in.DisplayName == "" {
		return in, fmt.Errorf("%w: display name is required", ErrValidation)
	}
	if in.Role != "administrator" && in.Role != "editor" && in.Role != "author" {
		return in, fmt.Errorf("%w: invalid role", ErrValidation)
	}
	if passwordRequired && len(in.Password) < 10 {
		return in, fmt.Errorf("%w: password must be at least 10 characters", ErrValidation)
	}
	return in, nil
}

func (s *Service) Create(ctx context.Context, in Input) error {
	var err error
	in, err = validate(in, true)
	if err != nil {
		return err
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	uid, err := id.New()
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.db.ExecContext(ctx, `INSERT INTO users(id,email,username,password_hash,display_name,role,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`, uid, in.Email, in.Username, hash, in.DisplayName, in.Role, now, now)
	if err != nil {
		return fmt.Errorf("%w: email or username is already in use", ErrValidation)
	}
	return nil
}

func (s *Service) Update(ctx context.Context, userID string, in Input) error {
	var err error
	in, err = validate(in, false)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var oldRole string
	if err := tx.QueryRowContext(ctx, `SELECT role FROM users WHERE id=?`, userID).Scan(&oldRole); err != nil {
		return err
	}
	if oldRole == "administrator" && in.Role != "administrator" {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE role='administrator'`).Scan(&n); err != nil {
			return err
		}
		if n <= 1 {
			return fmt.Errorf("%w: the last administrator cannot be demoted", ErrValidation)
		}
	}
	res, err := tx.ExecContext(ctx, `UPDATE users SET email=?,username=?,display_name=?,role=?,updated_at=? WHERE id=?`, in.Email, in.Username, in.DisplayName, in.Role, time.Now().UTC().Format(time.RFC3339Nano), userID)
	if err != nil {
		return fmt.Errorf("%w: email or username is already in use", ErrValidation)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return sql.ErrNoRows
	}
	if oldRole != in.Role {
		if _, err = tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id=?`, userID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Service) ResetPassword(ctx context.Context, userID, password string) error {
	if len(password) < 10 {
		return fmt.Errorf("%w: password must be at least 10 characters", ErrValidation)
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE users SET password_hash=?,updated_at=? WHERE id=?`, hash, time.Now().UTC().Format(time.RFC3339Nano), userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return sql.ErrNoRows
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id=?`, userID); err != nil {
		return err
	}
	return tx.Commit()
}
