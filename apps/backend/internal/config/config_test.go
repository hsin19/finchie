package config

import (
	"strings"
	"testing"
)

func TestAccessNeedsBothSettingsOrNeither(t *testing.T) {
	t.Setenv("BACKEND_DATABASE_URL", "postgres://localhost/finchie")

	cases := []struct {
		domain, aud string
		enabled     bool
		fails       bool
	}{
		{"", "", false, false},
		{"https://team.cloudflareaccess.com", "aud", true, false},
		{"https://team.cloudflareaccess.com", "", false, true},
		{"", "aud", false, true},
	}
	for _, tc := range cases {
		t.Setenv("BACKEND_ACCESS_TEAM_DOMAIN", tc.domain)
		t.Setenv("BACKEND_ACCESS_AUD", tc.aud)
		cfg, err := Load()
		if (err != nil) != tc.fails {
			t.Fatalf("domain=%q aud=%q: err = %v, want failure %v", tc.domain, tc.aud, err, tc.fails)
		}
		if err == nil && cfg.AccessEnabled() != tc.enabled {
			t.Fatalf("domain=%q aud=%q: enabled = %v", tc.domain, tc.aud, cfg.AccessEnabled())
		}
	}
}

func TestAnExchangeNeedsAllOfItsKeysOrNone(t *testing.T) {
	t.Setenv("BACKEND_DATABASE_URL", "postgres://localhost/finchie")

	cfg, err := Load()
	if err != nil || cfg.ExchangeEnabled("bitget") {
		t.Fatalf("no keys: enabled = %v, err = %v", cfg.ExchangeEnabled("bitget"), err)
	}
	t.Setenv("BACKEND_BITGET_API_KEY", "key")
	t.Setenv("BACKEND_BITGET_API_SECRET", "secret")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "BACKEND_BITGET_PASSPHRASE") {
		t.Fatalf("two of three keys: err = %v", err)
	}
	t.Setenv("BACKEND_BITGET_PASSPHRASE", "passphrase")
	cfg, err = Load()
	if err != nil || !cfg.ExchangeEnabled("bitget") || cfg.ExchangeEnabled("max") {
		t.Fatalf("all three keys: bitget %v, max %v, err = %v", cfg.ExchangeEnabled("bitget"), cfg.ExchangeEnabled("max"), err)
	}
}
