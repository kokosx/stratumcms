// Package styles owns validated global presentation settings and generated CSS.
package styles

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	store "github.com/kokosx/stratumcms/internal/storage/sqlc"
)

var ErrValidation = errors.New("site styles validation")

type Tokens struct {
	Background, Surface, Text, Muted, Brand, Border        string
	Body, Heading                                          string
	RadiusSmall, RadiusMedium, RadiusLarge, ContainerWidth string
}
type Settings struct {
	ActiveTheme string
	Tokens      Tokens
	CustomCSS   string
	Version     int64
	UpdatedAt   string
}
type Service struct {
	queries *store.Queries
	now     func() time.Time
}

func New(queries *store.Queries) *Service { return &Service{queries: queries, now: time.Now} }
func Defaults() Tokens {
	return Tokens{"#ffffff", "#f8fafc", "#172033", "#667085", "#2563eb", "#d0d5dd", "sans", "sans", "4px", "8px", "16px", "72rem"}
}
func (s *Service) Get(ctx context.Context) (Settings, error) {
	r, err := s.queries.GetSitePresentation(ctx)
	if err != nil {
		return Settings{}, fmt.Errorf("get site presentation: %w", err)
	}
	t := Defaults()
	if r.StylesJson != "" && r.StylesJson != "{}" {
		if err := json.Unmarshal([]byte(r.StylesJson), &t); err != nil {
			return Settings{}, fmt.Errorf("parse site styles: %w", err)
		}
	}
	return Settings{ActiveTheme: r.ActiveTheme, Tokens: t, CustomCSS: r.CustomCss, Version: r.Version, UpdatedAt: r.UpdatedAt}, nil
}
func (s *Service) Update(ctx context.Context, expected int64, active string, tokens Tokens, custom string) (Settings, error) {
	if err := Validate(tokens); err != nil {
		return Settings{}, err
	}
	if active == "" {
		return Settings{}, fmt.Errorf("%w: active theme is required", ErrValidation)
	}
	encoded, err := json.Marshal(tokens)
	if err != nil {
		return Settings{}, err
	}
	changed, err := s.queries.UpdateSitePresentation(ctx, store.UpdateSitePresentationParams{ActiveTheme: active, StylesJson: string(encoded), CustomCss: custom, UpdatedAt: s.now().UTC().Format(time.RFC3339Nano), Version: expected})
	if err != nil {
		return Settings{}, fmt.Errorf("update site presentation: %w", err)
	}
	if changed != 1 {
		return Settings{}, fmt.Errorf("%w: settings changed elsewhere", ErrValidation)
	}
	return s.Get(ctx)
}

var color = regexp.MustCompile(`^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)
var size = regexp.MustCompile(`^(?:0|[1-9][0-9]{0,3})(?:px|rem)$`)

func Validate(t Tokens) error {
	for _, v := range []string{t.Background, t.Surface, t.Text, t.Muted, t.Brand, t.Border} {
		if !color.MatchString(v) {
			return fmt.Errorf("%w: colors must be hex values", ErrValidation)
		}
	}
	for _, v := range []string{t.RadiusSmall, t.RadiusMedium, t.RadiusLarge, t.ContainerWidth} {
		if !size.MatchString(v) {
			return fmt.Errorf("%w: sizes must use px or rem", ErrValidation)
		}
	}
	for _, v := range []string{t.Body, t.Heading} {
		if v != "system" && v != "sans" && v != "serif" && v != "mono" {
			return fmt.Errorf("%w: unsupported font", ErrValidation)
		}
	}
	return nil
}
func font(name string) string {
	return map[string]string{"system": "system-ui, sans-serif", "sans": "Arial, Helvetica, sans-serif", "serif": "Georgia, serif", "mono": "ui-monospace, monospace"}[name]
}
func CSS(s Settings) string {
	t := s.Tokens
	var b strings.Builder
	fmt.Fprintf(&b, ":root{--stratum-color-background:%s;--stratum-color-surface:%s;--stratum-color-text:%s;--stratum-color-muted:%s;--stratum-color-brand:%s;--stratum-color-border:%s;--stratum-font-body:%s;--stratum-font-heading:%s;--stratum-radius-small:%s;--stratum-radius-medium:%s;--stratum-radius-large:%s;--stratum-container-width:%s}\n", t.Background, t.Surface, t.Text, t.Muted, t.Brand, t.Border, font(t.Body), font(t.Heading), t.RadiusSmall, t.RadiusMedium, t.RadiusLarge, t.ContainerWidth)
	return b.String() + s.CustomCSS
}
