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
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"github.com/kokosx/stratumcms/internal/auth"
	"github.com/kokosx/stratumcms/internal/authorization"
	"github.com/kokosx/stratumcms/internal/cache"
	"github.com/kokosx/stratumcms/internal/config"
	"github.com/kokosx/stratumcms/internal/content"
	"github.com/kokosx/stratumcms/internal/documents"
	"github.com/kokosx/stratumcms/internal/editor"
	"github.com/kokosx/stratumcms/internal/media"
	"github.com/kokosx/stratumcms/internal/menus"
	"github.com/kokosx/stratumcms/internal/migrations"
	"github.com/kokosx/stratumcms/internal/platform"
	"github.com/kokosx/stratumcms/internal/presentation"
	"github.com/kokosx/stratumcms/internal/publishing"
	"github.com/kokosx/stratumcms/internal/redirects"
	"github.com/kokosx/stratumcms/internal/renderer"
	store "github.com/kokosx/stratumcms/internal/storage/sqlc"
	"github.com/kokosx/stratumcms/internal/storage/turso"
	"github.com/kokosx/stratumcms/internal/styles"
	"github.com/kokosx/stratumcms/internal/themes"
	"github.com/kokosx/stratumcms/internal/users"
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
	themeRegistry, err := themes.NewRegistry()
	if err != nil {
		panic(fmt.Sprintf("load themes: %v", err))
	}
	stylesService := styles.New(store.New(db))
	dataDir := cfg.DataDir
	if dataDir == "" {
		dataDir = "./data"
	}
	mediaService, err := media.New(db, dataDir)
	if err != nil {
		panic(fmt.Sprintf("create media storage: %v", err))
	}
	rendererService := renderer.New(contentService.Registry(), mediaService)
	pages, err := cache.New(dataDir)
	if err != nil {
		panic(fmt.Sprintf("create page cache: %v", err))
	}
	menuService := menus.New(db)
	presentationService := presentation.New(rendererService, stylesService, themeRegistry, menuService)
	h := &handler{db: db, dataDir: dataDir, auth: newAuthService(db), users: users.New(db), content: contentService, editor: editor.New(db, contentService), presentation: presentationService, pages: pages, media: mediaService, menus: menuService, redirects: redirects.New(db), styles: stylesService, themes: themeRegistry, templates: templates, logger: logger, secureCookies: cfg.SecureCookies, limiter: newLoginLimiter()}
	h.publishing = publishing.New(contentService, pages, presentationService, logger)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", health)
	mux.HandleFunc("GET /ready", h.ready)
	mux.HandleFunc("GET /static/app.css", css)
	mux.HandleFunc("GET /assets/site.css", h.siteCSS)
	mux.HandleFunc("GET /assets/themes/{theme}/{asset...}", h.themeAsset)
	mux.HandleFunc("GET /static/editor.css", editorCSS)
	mux.HandleFunc("GET /static/datastar-1.0.0.js", datastarJS)
	mux.HandleFunc("GET /media/{id}/{filename}", h.publicMedia)
	mux.Handle("GET /admin/media", h.requireCapability(authorization.ManageMedia, http.HandlerFunc(h.mediaList)))
	mux.Handle("POST /admin/media", h.requireCapability(authorization.ManageMedia, http.HandlerFunc(h.mediaUpload)))
	mux.Handle("POST /admin/media/{id}/delete", h.requireCapability(authorization.ManageMedia, http.HandlerFunc(h.mediaDelete)))
	mux.Handle("POST /admin/media/{id}/metadata", h.requireCapability(authorization.ManageMedia, http.HandlerFunc(h.mediaMetadata)))
	mux.HandleFunc("GET /setup", h.setupForm)
	mux.HandleFunc("POST /setup", h.setup)
	mux.HandleFunc("GET /login", h.loginForm)
	mux.HandleFunc("POST /login", h.login)
	mux.HandleFunc("POST /logout", h.logout)
	mux.Handle("GET /admin", h.requireAuth(http.HandlerFunc(h.admin)))
	mux.Handle("GET /admin/posts", h.requireCapability(authorization.ManagePosts, http.HandlerFunc(h.entries("post"))))
	mux.Handle("GET /admin/posts/new", h.requireCapability(authorization.ManagePosts, http.HandlerFunc(h.newEntry("post"))))
	mux.Handle("POST /admin/posts", h.requireCapability(authorization.ManagePosts, http.HandlerFunc(h.createEntry("post"))))
	mux.Handle("GET /admin/posts/{id}/edit", h.requireCapability(authorization.ManagePosts, http.HandlerFunc(h.editEntry("post"))))
	mux.Handle("GET /admin/pages", h.requireCapability(authorization.ManagePages, http.HandlerFunc(h.entries("page"))))
	mux.Handle("GET /admin/pages/new", h.requireCapability(authorization.ManagePages, http.HandlerFunc(h.newEntry("page"))))
	mux.Handle("POST /admin/pages", h.requireCapability(authorization.ManagePages, http.HandlerFunc(h.createEntry("page"))))
	mux.Handle("GET /admin/pages/{id}/edit", h.requireCapability(authorization.ManagePages, http.HandlerFunc(h.editEntry("page"))))
	mux.Handle("GET /admin/appearance/themes", h.requireCapability(authorization.ManageAppearance, http.HandlerFunc(h.appearanceThemes)))
	mux.Handle("POST /admin/appearance/themes", h.requireCapability(authorization.ManageAppearance, http.HandlerFunc(h.saveTheme)))
	mux.Handle("GET /admin/appearance/styles", h.requireCapability(authorization.ManageAppearance, http.HandlerFunc(h.appearanceStyles)))
	mux.Handle("POST /admin/appearance/styles", h.requireCapability(authorization.ManageAppearance, http.HandlerFunc(h.saveStyles)))
	mux.Handle("GET /admin/appearance/menus", h.requireCapability(authorization.ManageMenus, http.HandlerFunc(h.menusPage)))
	mux.Handle("POST /admin/appearance/menus", h.requireCapability(authorization.ManageMenus, http.HandlerFunc(h.menuAdd)))
	mux.Handle("POST /admin/appearance/menus/{id}/move", h.requireCapability(authorization.ManageMenus, http.HandlerFunc(h.menuMove)))
	mux.Handle("POST /admin/appearance/menus/{id}/delete", h.requireCapability(authorization.ManageMenus, http.HandlerFunc(h.menuDelete)))
	mux.Handle("GET /admin/redirects", h.requireCapability(authorization.ManageRedirects, http.HandlerFunc(h.redirectsPage)))
	mux.Handle("POST /admin/redirects", h.requireCapability(authorization.ManageRedirects, http.HandlerFunc(h.redirectCreate)))
	mux.Handle("POST /admin/redirects/{id}/delete", h.requireCapability(authorization.ManageRedirects, http.HandlerFunc(h.redirectDelete)))
	mux.Handle("GET /admin/users", h.requireCapability(authorization.ManageUsers, http.HandlerFunc(h.userList)))
	mux.Handle("POST /admin/users", h.requireCapability(authorization.ManageUsers, http.HandlerFunc(h.userCreate)))
	mux.Handle("POST /admin/users/{id}", h.requireCapability(authorization.ManageUsers, http.HandlerFunc(h.userUpdate)))
	mux.Handle("POST /admin/users/{id}/password", h.requireCapability(authorization.ManageUsers, http.HandlerFunc(h.userPassword)))
	mux.Handle("GET /admin/editor/{id}", h.requireEntryAccess(http.HandlerFunc(h.editorPage(""))))
	mux.Handle("GET /admin/editor/{id}/preview", h.requireEntryAccess(http.HandlerFunc(h.editorPreview)))
	mux.Handle("GET /admin/editor/{id}/seo", h.requireEntryAccess(http.HandlerFunc(h.seoPage)))
	mux.Handle("POST /admin/editor/{id}/seo", h.requireEntryAccess(http.HandlerFunc(h.seoSave)))
	mux.Handle("POST /admin/editor/{id}/blocks", h.requireEntryAccess(http.HandlerFunc(h.editorAddBlock)))
	mux.Handle("POST /admin/editor/{id}/blocks/{nodeID}", h.requireEntryAccess(http.HandlerFunc(h.editorUpdateBlock)))
	mux.Handle("POST /admin/editor/{id}/blocks/{nodeID}/delete", h.requireEntryAccess(http.HandlerFunc(h.editorDeleteBlock)))
	mux.Handle("POST /admin/editor/{id}/blocks/{nodeID}/duplicate", h.requireEntryAccess(http.HandlerFunc(h.editorDuplicateBlock)))
	mux.Handle("POST /admin/editor/{id}/blocks/{nodeID}/move", h.requireEntryAccess(http.HandlerFunc(h.editorMoveBlock)))
	mux.Handle("POST /admin/editor/{id}/metadata", h.requireEntryAccess(http.HandlerFunc(h.editorMetadata)))
	mux.Handle("POST /admin/editor/{id}/save", h.requireEntryAccess(http.HandlerFunc(h.editorSave)))
	mux.Handle("POST /admin/editor/{id}/publish", h.requireCapability(authorization.PublishContent, http.HandlerFunc(h.editorPublish)))
	mux.HandleFunc("GET /{path...}", h.public)
	return h.recoverPanic(h.requestID(h.securityHeaders(h.requestLog(h.bodyLimit(mux)))))
}

func health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

type handler struct {
	db            *sql.DB
	dataDir       string
	auth          *authService
	users         *users.Service
	content       *content.Service
	editor        *editor.Service
	presentation  *presentation.Service
	pages         *cache.Pages
	publishing    *publishing.Service
	media         *media.Service
	menus         *menus.Service
	redirects     *redirects.Service
	templates     *template.Template
	styles        *styles.Service
	themes        *themes.Registry
	logger        *slog.Logger
	secureCookies bool
	limiter       *loginLimiter
}

func (h *handler) home(w http.ResponseWriter, r *http.Request) {
	configured, err := h.auth.configured(r.Context())
	if err != nil {
		h.internalError(w, err)
		return
	}
	if !configured {
		http.Redirect(w, r, "/setup", http.StatusSeeOther)
		return
	}
	if h.loggedIn(r) {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (h *handler) ready(w http.ResponseWriter, r *http.Request) {
	ready := true
	if err := h.db.PingContext(r.Context()); err != nil {
		ready = false
		h.logger.Error("readiness database", "error", err)
	}
	var version int
	if err := h.db.QueryRowContext(r.Context(), "SELECT COALESCE(MAX(version),0) FROM schema_migrations").Scan(&version); err != nil || version != migrations.LatestVersion() {
		ready = false
		h.logger.Error("readiness migrations", "version", version, "error", err)
	}
	settings, err := h.styles.Get(r.Context())
	if err != nil {
		ready = false
		h.logger.Error("readiness styles", "error", err)
	} else if _, ok := h.themes.Resolve(settings.ActiveTheme); !ok {
		ready = false
		h.logger.Error("readiness theme", "theme", settings.ActiveTheme)
	}
	for _, name := range []string{"media", "cache/pages", "tmp"} {
		if st, err := os.Stat(filepath.Join(h.dataDir, name)); err != nil || !st.IsDir() {
			ready = false
			h.logger.Error("readiness directory", "name", name, "error", err)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	if !ready {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"status": "not ready"})
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
}

func (h *handler) public(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/" {
		h.home(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/admin/") || strings.HasPrefix(r.URL.Path, "/static/") || strings.HasPrefix(r.URL.Path, "/assets/") || r.URL.Path == "/login" || r.URL.Path == "/setup" || r.URL.Path == "/health" || r.URL.Path == "/ready" {
		http.NotFound(w, r)
		return
	}
	path := r.URL.Path
	if cached, ok := h.pages.Get(path); ok {
		h.logger.Debug("page_cache_hit", "path", path)
		h.writePublic(w, r, cached.HTML, cached.ETag)
		return
	}
	h.logger.Debug("page_cache_miss", "path", path)
	published, err := h.content.ResolvePublished(r.Context(), path)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			rule, redirectErr := h.redirects.Get(r.Context(), path)
			if redirectErr == nil {
				http.Redirect(w, r, rule.ToPath, rule.StatusCode)
				return
			}
			if errors.Is(redirectErr, sql.ErrNoRows) {
				http.NotFound(w, r)
			} else {
				h.internalError(w, redirectErr)
			}
		} else {
			h.internalError(w, err)
		}
		return
	}
	if published.SEO.Canonical == "" {
		published.SEO.Canonical = published.Path
	}
	if published.SEO.Robots == "" {
		published.SEO.Robots = "index,follow"
	}
	result, err := h.presentation.Render(r.Context(), published.Kind, published.Title, published.Document, published.SEO)
	if err != nil {
		h.logger.Error("render published page", "error", err)
		h.internalError(w, err)
		return
	}
	etag := publishing.ETag(result.HTML)
	entry := cache.Entry{Path: published.Path, HTML: result.HTML, ETag: etag, Dependencies: pageDependencies(published.Document, published.EntryID, published.RevisionID, published.Path, result.ThemeID)}
	if err := h.pages.Put(entry); err != nil {
		h.logger.Error("page_cache_write", "path", path, "error", err)
	} else {
		h.logger.Debug("page_cache_write", "path", path)
	}
	h.writePublic(w, r, result.HTML, etag)
}

func pageDependencies(document documents.Document, entryID, revisionID, route, themeID string) []string {
	deps := []string{"entry:" + entryID, "revision:" + revisionID, "theme:" + themeID, "presentation", "route:" + route}
	seen := map[string]bool{}
	var walk func([]documents.Node)
	walk = func(nodes []documents.Node) {
		for _, n := range nodes {
			if n.Type == "core.image" {
				if mediaID, ok := n.Props["media"].(string); ok && mediaID != "" && !seen[mediaID] {
					deps = append(deps, "media:"+mediaID)
					seen[mediaID] = true
				}
			}
			walk(n.Children)
		}
	}
	walk(document.Children)
	return deps
}

func (h *handler) writePublic(w http.ResponseWriter, r *http.Request, body []byte, etag string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=60")
	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(body)
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
	limitKey := clientKey(r, r.FormValue("email"))
	if !h.limiter.Allow(limitKey) {
		h.formError(w, "setup", formData{}, "Please wait before trying again.")
		return
	}
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
	h.limiter.Success(limitKey)
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
	limitKey := clientKey(r, data.Login)
	if !h.limiter.Allow(limitKey) {
		h.formError(w, "login", data, "Invalid login or password.")
		return
	}
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
	h.limiter.Success(limitKey)
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
		start := time.Now()
		rw := &responseRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)
		if !strings.HasPrefix(r.URL.Path, "/static/") && !strings.HasPrefix(r.URL.Path, "/assets/") {
			h.logger.Info("HTTP request", "method", r.Method, "path", r.URL.Path, "status", rw.status, "bytes", rw.bytes, "duration", time.Since(start), "request_id", requestIDFrom(r.Context()))
		}
	})
}
func (h *handler) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data: blob:; connect-src 'self'; frame-src 'self'; frame-ancestors 'self'; object-src 'none'; base-uri 'none'; form-action 'self'")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
		if r.URL.Path == "/setup" || r.URL.Path == "/login" || r.URL.Path == "/admin" || strings.HasPrefix(r.URL.Path, "/admin/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		w.Header().Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

type requestIDKey struct{}

func requestIDFrom(ctx context.Context) string { v, _ := ctx.Value(requestIDKey{}).(string); return v }
func (h *handler) requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := auth.NewToken()
		if err != nil {
			id = fmt.Sprintf("%d", time.Now().UnixNano())
		}
		if len(id) > 20 {
			id = id[:20]
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id)))
	})
}
func (h *handler) recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				h.logger.Error("request panic", "panic", v, "stack", string(debug.Stack()), "request_id", requestIDFrom(r.Context()))
				http.Error(w, "Internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
func (h *handler) bodyLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			limit := int64(1 << 20)
			if r.URL.Path == "/admin/media" {
				limit = media.MaxUploadSize + (1 << 20)
			}
			r.Body = http.MaxBytesReader(w, r.Body, limit)
			contentType := r.Header.Get("Content-Type")
			if !strings.HasPrefix(contentType, "multipart/") && !strings.HasPrefix(contentType, "application/json") {
				if err := r.ParseForm(); err != nil {
					http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

type responseRecorder struct {
	http.ResponseWriter
	status, bytes int
}

func (r *responseRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}
func (r *responseRecorder) Write(p []byte) (int, error) {
	n, e := r.ResponseWriter.Write(p)
	r.bytes += n
	return n, e
}
func (r *responseRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
