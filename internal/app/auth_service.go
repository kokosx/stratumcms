package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kokosx/stratumcms/internal/auth"
	"github.com/kokosx/stratumcms/internal/id"
	store "github.com/kokosx/stratumcms/internal/storage/sqlc"
)

const sessionLifetime = 30 * 24 * time.Hour

var ErrSetupComplete = errors.New("setup already complete")
var ErrInvalidCredentials = errors.New("invalid credentials")

type authService struct {
	db      *sql.DB
	queries *store.Queries
	now     func() time.Time
}

func newAuthService(db *sql.DB) *authService {
	return &authService{db: db, queries: store.New(db), now: time.Now}
}

type setupInput struct{ Email, Username, DisplayName, Password string }

func (s *authService) setup(ctx context.Context, input setupInput) (string, error) {
	if err := validateSetup(input); err != nil {
		return "", err
	}
	hash, err := auth.HashPassword(input.Password)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("begin setup: %w", err)
	}
	defer tx.Rollback()
	now := s.now().UTC()
	q := s.queries.WithTx(tx)
	if err := q.CreateInstallation(ctx, timestamp(now)); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "installation.id") || strings.Contains(strings.ToLower(err.Error()), "unique") {
			return "", ErrSetupComplete
		}
		return "", fmt.Errorf("create installation: %w", err)
	}
	userID, err := id.New()
	if err != nil {
		return "", fmt.Errorf("generate user id: %w", err)
	}
	if err := q.CreateUser(ctx, store.CreateUserParams{ID: userID, Email: normalizeLogin(input.Email), Username: normalizeLogin(input.Username), PasswordHash: hash, DisplayName: strings.TrimSpace(input.DisplayName), Role: "administrator", CreatedAt: timestamp(now), UpdatedAt: timestamp(now)}); err != nil {
		return "", fmt.Errorf("create administrator: %w", err)
	}
	token, err := s.createSession(ctx, q, userID, now)
	if err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit setup: %w", err)
	}
	return token, nil
}

func (s *authService) login(ctx context.Context, login, password string) (string, error) {
	login = normalizeLogin(login)
	var user store.User
	var err error
	if strings.Contains(login, "@") {
		user, err = s.queries.GetUserByEmail(ctx, login)
	} else {
		user, err = s.queries.GetUserByUsername(ctx, login)
	}
	if err != nil || !auth.VerifyPassword(user.PasswordHash, password) {
		return "", ErrInvalidCredentials
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("begin login: %w", err)
	}
	defer tx.Rollback()
	token, err := s.createSession(ctx, s.queries.WithTx(tx), user.ID, s.now().UTC())
	if err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit login: %w", err)
	}
	return token, nil
}

func (s *authService) createSession(ctx context.Context, q *store.Queries, userID string, now time.Time) (string, error) {
	token, err := auth.NewToken()
	if err != nil {
		return "", fmt.Errorf("generate session token: %w", err)
	}
	id, err := id.New()
	if err != nil {
		return "", fmt.Errorf("generate session id: %w", err)
	}
	if err := q.CreateSession(ctx, store.CreateSessionParams{ID: id, UserID: userID, TokenHash: auth.HashToken(token), ExpiresAt: timestamp(now.Add(sessionLifetime)), CreatedAt: timestamp(now), LastSeenAt: timestamp(now)}); err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}
	return token, nil
}

func (s *authService) currentUser(ctx context.Context, token string) (store.User, error) {
	return s.queries.GetUserBySessionTokenHash(ctx, store.GetUserBySessionTokenHashParams{TokenHash: auth.HashToken(token), ExpiresAt: timestamp(s.now().UTC())})
}
func (s *authService) logout(ctx context.Context, token string) error {
	if err := s.queries.DeleteSessionByTokenHash(ctx, auth.HashToken(token)); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}
func (s *authService) configured(ctx context.Context) (bool, error) {
	configured, err := s.queries.IsConfigured(ctx)
	return configured != 0, err
}
func timestamp(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }
func validateSetup(in setupInput) error {
	if !strings.Contains(strings.TrimSpace(in.Email), "@") {
		return errors.New("enter a valid email address")
	}
	username := normalizeLogin(in.Username)
	if len(username) < 3 {
		return errors.New("username must be at least 3 characters")
	}
	if strings.Contains(username, "@") {
		return errors.New("username cannot contain @")
	}
	if strings.TrimSpace(in.DisplayName) == "" {
		return errors.New("display name is required")
	}
	if len(in.Password) < 10 {
		return errors.New("password must be at least 10 characters")
	}
	return nil
}

func normalizeLogin(value string) string { return strings.ToLower(strings.TrimSpace(value)) }
