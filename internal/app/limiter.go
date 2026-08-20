package app

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type loginAttempt struct {
	count  int
	window time.Time
}
type loginLimiter struct {
	mu       sync.Mutex
	attempts map[string]loginAttempt
	now      func() time.Time
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{attempts: map[string]loginAttempt{}, now: time.Now}
}
func (l *loginLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	a := l.attempts[key]
	if a.window.IsZero() || now.Sub(a.window) > 10*time.Minute {
		a = loginAttempt{window: now}
	}
	a.count++
	l.attempts[key] = a
	if len(l.attempts) > 2048 {
		for k, v := range l.attempts {
			if now.Sub(v.window) > 10*time.Minute {
				delete(l.attempts, k)
			}
		}
	}
	return a.count <= 10
}
func (l *loginLimiter) Success(key string) { l.mu.Lock(); delete(l.attempts, key); l.mu.Unlock() }
func clientKey(r *http.Request, login string) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return host + "|" + strings.ToLower(strings.TrimSpace(login))
}
