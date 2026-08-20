package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/kokosx/stratumcms/internal/config"
	"github.com/kokosx/stratumcms/internal/content"
	"github.com/kokosx/stratumcms/internal/migrations"
	store "github.com/kokosx/stratumcms/internal/storage/sqlc"
	"github.com/kokosx/stratumcms/internal/storage/turso"
)

func TestSetupAllowsOnlyOneAdministrator(t *testing.T) {
	s := testServer(t)
	defer s.Close()
	client := testClient(t)
	setup(t, client, s.URL, "admin@example.test", "admin")
	resp := get(t, client, s.URL+"/setup")
	if resp.Request.URL.Path != "/admin" {
		t.Fatalf("setup after completion ended at %s", resp.Request.URL.Path)
	}
}

func TestUnauthenticatedAdminRedirectsToLogin(t *testing.T) {
	s := testServer(t)
	defer s.Close()
	resp := get(t, testClient(t), s.URL+"/admin")
	if resp.Request.URL.Path != "/login" {
		t.Fatalf("admin ended at %s", resp.Request.URL.Path)
	}
}

func TestUnauthenticatedPagesRedirectToLogin(t *testing.T) {
	s := testServer(t)
	defer s.Close()
	resp := get(t, testClient(t), s.URL+"/admin/pages")
	if resp.Request.URL.Path != "/login" {
		t.Fatalf("pages ended at %s", resp.Request.URL.Path)
	}
}

func TestLoginAndLogout(t *testing.T) {
	s := testServer(t)
	defer s.Close()
	client := testClient(t)
	setup(t, client, s.URL, "admin@example.test", "admin")
	logout(t, client, s.URL)
	resp := get(t, client, s.URL+"/admin")
	if resp.Request.URL.Path != "/login" {
		t.Fatal("logged-out session accessed admin")
	}
	login(t, client, s.URL, "admin", "a long test password")
	resp = get(t, client, s.URL+"/admin")
	if resp.Request.URL.Path != "/admin" {
		t.Fatalf("login ended at %s", resp.Request.URL.Path)
	}
}

func TestEmailLoginIsCaseInsensitive(t *testing.T) {
	s := testServer(t)
	defer s.Close()
	client := testClient(t)
	setup(t, client, s.URL, "admin@example.test", "admin")
	logout(t, client, s.URL)
	login(t, client, s.URL, "ADMIN@EXAMPLE.TEST", "a long test password")
}

func TestConcurrentSetupCreatesExactlyOneAdministrator(t *testing.T) {
	ctx := context.Background()
	db, err := turso.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := migrations.Run(ctx, db); err != nil {
		t.Fatal(err)
	}
	service := newAuthService(db)
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func(i int) {
			_, err := service.setup(ctx, setupInput{Email: fmt.Sprintf("admin%d@example.test", i), Username: fmt.Sprintf("admin%d", i), DisplayName: "Administrator", Password: "a long test password"})
			results <- err
		}(i)
	}
	winners := 0
	for i := 0; i < 2; i++ {
		if err := <-results; err == nil {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("successful setups = %d, want 1", winners)
	}
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users WHERE role='administrator'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("administrators=%d", count)
	}
}

func TestPublicRouteUsesPublishedRevisionAndAdminKindIsEnforced(t *testing.T) {
	s := testServer(t)
	defer s.Close()
	client := testClient(t)
	setup(t, client, s.URL, "admin@example.test", "admin")
	token := csrf(t, client, s.URL+"/admin/pages/new")
	created := post(t, client, s.URL+"/admin/pages", url.Values{"csrf_token": {token}, "title": {"About"}, "slug": {"about"}})
	if created.Request.URL.Path == "/admin/pages" {
		t.Fatal("page creation did not redirect")
	}
	pageID := strings.Split(created.Request.URL.Path, "/")[3]
	public := get(t, client, s.URL+"/about")
	if public.StatusCode != http.StatusNotFound {
		t.Fatalf("draft public status=%d", public.StatusCode)
	}
	wrongKind := get(t, client, s.URL+"/admin/posts/"+pageID+"/edit")
	if wrongKind.StatusCode != http.StatusNotFound {
		t.Fatalf("post URL could edit page: %d", wrongKind.StatusCode)
	}
	token = csrf(t, client, s.URL+"/admin/pages/"+pageID+"/edit")
	published := post(t, client, s.URL+"/admin/editor/"+pageID+"/publish", url.Values{"csrf_token": {token}, "version": {"1"}})
	if published.Request.URL.Path != "/admin/pages/"+pageID+"/edit" {
		t.Fatalf("publish ended at %s", published.Request.URL.Path)
	}
	public = get(t, client, s.URL+"/about")
	if public.StatusCode != http.StatusOK {
		t.Fatalf("published public status=%d", public.StatusCode)
	}
	body, err := io.ReadAll(public.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "<title>About</title>") {
		t.Fatalf("missing public layout title: %s", body)
	}
	if !strings.Contains(string(body), "/assets/themes/starter/theme.css") || !strings.Contains(string(body), "/assets/site.css?v=") {
		t.Fatalf("public page is not using the presentation pipeline: %s", body)
	}
	preview := get(t, client, s.URL+"/admin/editor/"+pageID+"/preview")
	previewBody, err := io.ReadAll(preview.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(previewBody), "/assets/themes/starter/theme.css") || !strings.Contains(string(previewBody), "/assets/site.css?v=") {
		t.Fatalf("preview is not using the presentation pipeline: %s", previewBody)
	}
}

func TestPublicPageCacheServesHitWithoutDatabaseAndSupportsHEAD(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	db, err := turso.Open(ctx, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrations.Run(ctx, db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := store.New(db).CreateUser(ctx, store.CreateUserParams{ID: "author", Email: "author@example.test", Username: "author", PasswordHash: "hash", DisplayName: "Author", Role: "administrator", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	service := content.New(db)
	entry, err := service.CreateEntry(ctx, "page", "author", content.Input{Title: "About"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.PublishEntry(ctx, entry.ID, "author", content.Input{Title: "About"}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewHandler(db, slog.New(slog.NewTextHandler(io.Discard, nil)), config.Config{DataDir: dataDir}))
	defer server.Close()
	first, err := http.Get(server.URL + "/about")
	if err != nil {
		t.Fatal(err)
	}
	first.Body.Close()
	if first.StatusCode != http.StatusOK || first.Header.Get("ETag") == "" {
		t.Fatalf("first response: %d %#v", first.StatusCode, first.Header)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := http.Get(server.URL + "/about")
	if err != nil {
		t.Fatal(err)
	}
	defer second.Body.Close()
	if second.StatusCode != http.StatusOK {
		t.Fatalf("cache hit status = %d", second.StatusCode)
	}
	req, err := http.NewRequest(http.MethodHead, server.URL+"/about", nil)
	if err != nil {
		t.Fatal(err)
	}
	head, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer head.Body.Close()
	body, err := io.ReadAll(head.Body)
	if err != nil {
		t.Fatal(err)
	}
	if head.StatusCode != http.StatusOK || len(body) != 0 || head.Header.Get("ETag") != first.Header.Get("ETag") {
		t.Fatalf("head = %d body=%d headers=%#v", head.StatusCode, len(body), head.Header)
	}
}

func testServer(t *testing.T) *httptest.Server {
	t.Helper()
	dataDir := t.TempDir()
	db, err := turso.Open(context.Background(), dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := migrations.Run(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return httptest.NewServer(NewHandler(db, slog.New(slog.NewTextHandler(io.Discard, nil)), config.Config{DataDir: dataDir}))
}
func testClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Jar: jar}
}
func get(t *testing.T, c *http.Client, target string) *http.Response {
	t.Helper()
	r, err := c.Get(target)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Body.Close() })
	return r
}
func csrf(t *testing.T, c *http.Client, target string) string {
	t.Helper()
	get(t, c, target)
	u, _ := url.Parse(target)
	for _, cookie := range c.Jar.Cookies(u) {
		if cookie.Name == csrfCookie {
			return cookie.Value
		}
	}
	t.Fatal("missing csrf cookie")
	return ""
}
func post(t *testing.T, c *http.Client, target string, values url.Values) *http.Response {
	t.Helper()
	r, err := c.PostForm(target, values)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Body.Close() })
	return r
}
func setup(t *testing.T, c *http.Client, base, email, username string) {
	t.Helper()
	token := csrf(t, c, base+"/setup")
	r := post(t, c, base+"/setup", url.Values{"csrf_token": {token}, "email": {email}, "username": {username}, "display_name": {"Administrator"}, "password": {"a long test password"}, "confirm_password": {"a long test password"}})
	if r.Request.URL.Path != "/admin" {
		t.Fatalf("setup ended at %s", r.Request.URL.Path)
	}
}
func login(t *testing.T, c *http.Client, base, username, password string) {
	t.Helper()
	token := csrf(t, c, base+"/login")
	r := post(t, c, base+"/login", url.Values{"csrf_token": {token}, "login": {username}, "password": {password}})
	if r.Request.URL.Path != "/admin" {
		t.Fatalf("login ended at %s", r.Request.URL.Path)
	}
}
func logout(t *testing.T, c *http.Client, base string) {
	t.Helper()
	token := csrf(t, c, base+"/admin")
	r := post(t, c, base+"/logout", url.Values{"csrf_token": {token}})
	if r.Request.URL.Path != "/login" {
		t.Fatalf("logout ended at %s", r.Request.URL.Path)
	}
}
