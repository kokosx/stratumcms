package app

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/kokosx/stratumcms/internal/auth"
	"github.com/kokosx/stratumcms/internal/config"
	"github.com/kokosx/stratumcms/internal/content"
	"github.com/kokosx/stratumcms/internal/editor"
	"github.com/kokosx/stratumcms/internal/migrations"
	"github.com/kokosx/stratumcms/internal/platform"
	"github.com/kokosx/stratumcms/internal/renderer"
	store "github.com/kokosx/stratumcms/internal/storage/sqlc"
	"github.com/kokosx/stratumcms/internal/storage/turso"
)

const shutdownTimeout = 10 * time.Second

// Run initializes the application and serves HTTP until ctx is canceled.
func Run(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	logger.Info("starting Stratum", "data_dir", cfg.DataDir, "http_addr", cfg.Addr)
	if err := platform.EnsureDataDir(cfg.DataDir); err != nil {
		return err
	}

	db, err := turso.Open(ctx, cfg.DataDir)
	if err != nil {
		return err
	}
	defer db.Close()

	if err := migrations.Run(ctx, db); err != nil {
		logger.Error("migration error", "error", err)
		return err
	}

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           NewHandler(db, logger, cfg),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	listener, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.Addr, err)
	}
	logger.Info("HTTP server listening", "address", listener.Addr().String())

	serverErr := make(chan error, 1)
	go func() { serverErr <- server.Serve(listener) }()

	select {
	case err := <-serverErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve HTTP: %w", err)
	case <-ctx.Done():
		logger.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown HTTP server: %w", err)
		}
		return nil
	}
}

// NewHandler constructs the HTTP application using the supplied database.
func NewHandler(db *sql.DB, logger *slog.Logger, cfg config.Config) http.Handler {
	templates, err := parseTemplates()
	if err != nil {
		panic(fmt.Sprintf("parse embedded templates: %v", err))
	}
	contentService := content.New(db)
	publicTemplate := template.Must(template.New("public").Parse(`<!doctype html><html><head><meta charset="utf-8"><title>{{.Title}}</title></head><body><main>{{.Content}}</main></body></html>`))
	h := &handler{auth: newAuthService(db), content: contentService, editor: editor.New(db, contentService), renderer: renderer.New(contentService.Registry()), templates: templates, publicTemplate: publicTemplate, logger: logger, secureCookies: cfg.SecureCookies}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", health)
	mux.HandleFunc("GET /static/app.css", css)
	mux.HandleFunc("GET /static/editor.css", editorCSS)
	mux.HandleFunc("GET /static/datastar-1.0.0.js", datastarJS)
	mux.HandleFunc("GET /setup", h.setupForm)
	mux.HandleFunc("POST /setup", h.setup)
	mux.HandleFunc("GET /login", h.loginForm)
	mux.HandleFunc("POST /login", h.login)
	mux.HandleFunc("POST /logout", h.logout)
	mux.Handle("GET /admin", h.requireAuth(http.HandlerFunc(h.admin)))
	mux.Handle("GET /admin/posts", h.requireAuth(http.HandlerFunc(h.entries("post"))))
	mux.Handle("GET /admin/posts/new", h.requireAuth(http.HandlerFunc(h.newEntry("post"))))
	mux.Handle("POST /admin/posts", h.requireAuth(http.HandlerFunc(h.createEntry("post"))))
	mux.Handle("GET /admin/posts/{id}/edit", h.requireAuth(http.HandlerFunc(h.editEntry("post"))))
	mux.Handle("POST /admin/posts/{id}", h.requireAuth(http.HandlerFunc(h.updateEntry("post"))))
	mux.Handle("GET /admin/pages", h.requireAuth(http.HandlerFunc(h.entries("page"))))
	mux.Handle("GET /admin/pages/new", h.requireAuth(http.HandlerFunc(h.newEntry("page"))))
	mux.Handle("POST /admin/pages", h.requireAuth(http.HandlerFunc(h.createEntry("page"))))
	mux.Handle("GET /admin/pages/{id}/edit", h.requireAuth(http.HandlerFunc(h.editEntry("page"))))
	mux.Handle("POST /admin/pages/{id}", h.requireAuth(http.HandlerFunc(h.updateEntry("page"))))
	mux.Handle("GET /admin/editor/{id}", h.requireAuth(http.HandlerFunc(h.editorPage(""))))
	mux.Handle("GET /admin/editor/{id}/preview", h.requireAuth(http.HandlerFunc(h.editorPreview)))
	mux.Handle("POST /admin/editor/{id}/blocks", h.requireAuth(http.HandlerFunc(h.editorAddBlock)))
	mux.Handle("POST /admin/editor/{id}/blocks/{nodeID}", h.requireAuth(http.HandlerFunc(h.editorUpdateBlock)))
	mux.Handle("POST /admin/editor/{id}/blocks/{nodeID}/delete", h.requireAuth(http.HandlerFunc(h.editorDeleteBlock)))
	mux.Handle("POST /admin/editor/{id}/blocks/{nodeID}/duplicate", h.requireAuth(http.HandlerFunc(h.editorDuplicateBlock)))
	mux.Handle("POST /admin/editor/{id}/blocks/{nodeID}/move", h.requireAuth(http.HandlerFunc(h.editorMoveBlock)))
	mux.Handle("POST /admin/editor/{id}/metadata", h.requireAuth(http.HandlerFunc(h.editorMetadata)))
	mux.Handle("POST /admin/editor/{id}/save", h.requireAuth(http.HandlerFunc(h.editorSave)))
	mux.Handle("POST /admin/editor/{id}/publish", h.requireAuth(http.HandlerFunc(h.editorPublish)))
	mux.HandleFunc("GET /{path...}", h.public)
	return h.securityHeaders(h.requestLog(mux))
}

func health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

type handler struct {
	auth           *authService
	content        *content.Service
	editor         *editor.Service
	renderer       *renderer.Renderer
	templates      *template.Template
	publicTemplate *template.Template
	logger         *slog.Logger
	secureCookies  bool
}

func (h *handler) public(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/" || strings.HasPrefix(r.URL.Path, "/admin/") || strings.HasPrefix(r.URL.Path, "/static/") || r.URL.Path == "/login" || r.URL.Path == "/setup" || r.URL.Path == "/health" {
		http.NotFound(w, r)
		return
	}
	published, err := h.content.ResolvePublished(r.Context(), r.URL.Path)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
		} else {
			h.internalError(w, err)
		}
		return
	}
	body, err := h.renderer.Render(published.Document)
	if err != nil {
		h.logger.Error("render published document", "error", err)
		h.internalError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.publicTemplate.Execute(w, struct {
		Title   string
		Content template.HTML
	}{published.Title, body}); err != nil {
		h.logger.Error("render public template", "error", err)
	}
}

type formData struct {
	CSRFToken, Error, Email, Username, DisplayName, Login string
	User                                                  userView
}

const sessionCookie = "stratum_session"
const csrfCookie = "stratum_csrf"

func (h *handler) setupForm(w http.ResponseWriter, r *http.Request) {
	configured, err := h.auth.configured(r.Context())
	if err != nil {
		h.internalError(w, err)
		return
	}
	if configured {
		if h.loggedIn(r) {
			http.Redirect(w, r, "/admin", http.StatusSeeOther)
		} else {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
		}
		return
	}
	h.render(w, "setup", formData{CSRFToken: h.csrfToken(w, r)})
}
func (h *handler) setup(w http.ResponseWriter, r *http.Request) {
	if !h.validCSRF(r) {
		h.formError(w, "setup", formData{}, "Your form expired. Please try again.")
		return
	}
	in := setupInput{Email: r.FormValue("email"), Username: r.FormValue("username"), DisplayName: r.FormValue("display_name"), Password: r.FormValue("password")}
	data := formData{Email: in.Email, Username: in.Username, DisplayName: in.DisplayName}
	if in.Password != r.FormValue("confirm_password") {
		h.formError(w, "setup", data, "Passwords do not match.")
		return
	}
	token, err := h.auth.setup(r.Context(), in)
	if err != nil {
		if errors.Is(err, ErrSetupComplete) {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		h.formError(w, "setup", data, errorMessage(err))
		return
	}
	h.setSession(w, token)
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}
func (h *handler) loginForm(w http.ResponseWriter, r *http.Request) {
	if h.loggedIn(r) {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}
	h.render(w, "login", formData{CSRFToken: h.csrfToken(w, r)})
}
func (h *handler) login(w http.ResponseWriter, r *http.Request) {
	data := formData{Login: r.FormValue("login")}
	if !h.validCSRF(r) {
		h.formError(w, "login", data, "Your form expired. Please try again.")
		return
	}
	token, err := h.auth.login(r.Context(), data.Login, r.FormValue("password"))
	if err != nil {
		if errors.Is(err, ErrInvalidCredentials) {
			h.formError(w, "login", data, "Invalid login or password.")
			return
		}
		h.internalError(w, err)
		return
	}
	h.setSession(w, token)
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}
func (h *handler) logout(w http.ResponseWriter, r *http.Request) {
	if !h.validCSRF(r) {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	if token, err := r.Cookie(sessionCookie); err == nil {
		if err := h.auth.logout(r.Context(), token.Value); err != nil {
			h.internalError(w, err)
			return
		}
	}
	h.clearSession(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
func (h *handler) admin(w http.ResponseWriter, r *http.Request) {
	h.render(w, "admin", formData{User: currentUser(r), CSRFToken: h.csrfToken(w, r)})
}

func (h *handler) user(r *http.Request) (store.User, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return store.User{}, false
	}
	user, err := h.auth.currentUser(r.Context(), c.Value)
	return user, err == nil
}
func (h *handler) loggedIn(r *http.Request) bool { _, ok := h.user(r); return ok }
func (h *handler) render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.templates.ExecuteTemplate(w, name, data); err != nil {
		h.logger.Error("render template", "error", err)
	}
}
func (h *handler) formError(w http.ResponseWriter, name string, data formData, message string) {
	data.Error = message
	data.CSRFToken = h.csrfToken(w, nil)
	h.render(w, name, data)
}
func (h *handler) internalError(w http.ResponseWriter, err error) {
	h.logger.Error("internal server error", "error", err)
	http.Error(w, "Internal server error", http.StatusInternalServerError)
}

func errorMessage(err error) string {
	// Setup validation errors are deliberately safe to display; database details are not.
	if strings.HasPrefix(err.Error(), "create administrator:") || strings.HasPrefix(err.Error(), "hash password:") {
		return "Unable to create the administrator. Please try again."
	}
	return err.Error()
}
func (h *handler) csrfToken(w http.ResponseWriter, r *http.Request) string {
	if r != nil {
		if c, err := r.Cookie(csrfCookie); err == nil {
			return c.Value
		}
	}
	token, err := auth.NewToken()
	if err != nil {
		return ""
	}
	http.SetCookie(w, &http.Cookie{Name: csrfCookie, Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: h.secureCookies, MaxAge: int(sessionLifetime.Seconds())})
	return token
}
func (h *handler) validCSRF(r *http.Request) bool {
	c, err := r.Cookie(csrfCookie)
	return err == nil && c.Value != "" && subtle.ConstantTimeCompare([]byte(c.Value), []byte(r.FormValue("csrf_token"))) == 1
}
func (h *handler) setSession(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: h.secureCookies, MaxAge: int(sessionLifetime.Seconds())})
}
func (h *handler) clearSession(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: h.secureCookies, MaxAge: -1})
}
func (h *handler) requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.logger.Info("HTTP request", "method", r.Method, "path", r.URL.Path)
		next.ServeHTTP(w, r)
	})
}
func (h *handler) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		if r.URL.Path == "/setup" || r.URL.Path == "/login" || r.URL.Path == "/admin" || strings.HasPrefix(r.URL.Path, "/admin/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		w.Header().Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}
