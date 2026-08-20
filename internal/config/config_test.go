package config

import "testing"

func TestLoadRejectsExplicitInvalidValues(t *testing.T) {
	t.Setenv("STRATUM_SECURE_COOKIES", "sometimes")
	if _, err := Load(); err == nil {
		t.Fatal("invalid boolean accepted")
	}
	t.Setenv("STRATUM_SECURE_COOKIES", "")
	t.Setenv("STRATUM_ADDR", "not an address")
	if _, err := Load(); err == nil {
		t.Fatal("invalid address accepted")
	}
}
