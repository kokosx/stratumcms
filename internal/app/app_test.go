package app

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/kokosx/stratumcms/internal/config"
	"github.com/kokosx/stratumcms/internal/migrations"
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

func testServer(t *testing.T) *httptest.Server {
	t.Helper()
	db, err := turso.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := migrations.Run(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return httptest.NewServer(NewHandler(db, slog.New(slog.NewTextHandler(io.Discard, nil)), config.Config{}))
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
