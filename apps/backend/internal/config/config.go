// Package config reads the backend's settings from BACKEND_* environment variables.
package config

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
)

type Config struct {
	DatabaseURL string `env:"DATABASE_URL,required"`
	HTTPAddr    string `env:"HTTP_ADDR" envDefault:":8080"`
	ParserAddr  string `env:"PARSER_ADDR" envDefault:"localhost:50051"`
	// The parser needs a few seconds per statement, longer with OCR (ADR 0005).
	ParseTimeout time.Duration `env:"PARSE_TIMEOUT" envDefault:"120s"`
	// How many documents are parsed at once; keep it below the parser's workers, so previews and
	// health checks always get a thread.
	ParseConcurrency int    `env:"PARSE_CONCURRENCY" envDefault:"2"`
	MaxUploadMB      int    `env:"MAX_UPLOAD_MB" envDefault:"32"`
	LogFormat        string `env:"LOG_FORMAT" envDefault:"json"`
	// Cloudflare Access (ADR 0008): the team domain (https://<team>.cloudflareaccess.com) and the
	// application's AUD tag. Set both and every request but /healthz needs a valid Access token;
	// leave both empty for local development.
	AccessTeamDomain string `env:"ACCESS_TEAM_DOMAIN"`
	AccessAudience   string `env:"ACCESS_AUD"`
	// The address the mailbox Worker receives statements at (ADR 0010). Only the Gmail filter
	// download uses it (ADR 0014); it is the deployment's, never in the repository.
	MailboxAddress string `env:"MAILBOX_ADDRESS"`
	// Crypto exchanges' read-only API keys (ADR 0018). An exchange is fetched once all of its are set.
	MaxAccessKey     string `env:"MAX_ACCESS_KEY"`
	MaxSecretKey     string `env:"MAX_SECRET_KEY"`
	BinanceAPIKey    string `env:"BINANCE_API_KEY"`
	BinanceAPISecret string `env:"BINANCE_API_SECRET"`
	BitgetAPIKey     string `env:"BITGET_API_KEY"`
	BitgetAPISecret  string `env:"BITGET_API_SECRET"`
	BitgetPassphrase string `env:"BITGET_PASSPHRASE"`
}

func Load() (Config, error) {
	cfg, err := env.ParseAsWithOptions[Config](env.Options{Prefix: "BACKEND_"})
	if err != nil {
		return Config{}, fmt.Errorf("load config: %w", err)
	}
	// Half a setting would look like protection while checking nothing.
	if (cfg.AccessTeamDomain == "") != (cfg.AccessAudience == "") {
		return Config{}, errors.New("load config: set both BACKEND_ACCESS_TEAM_DOMAIN and BACKEND_ACCESS_AUD, or neither")
	}
	// Half an exchange's keys would leave it unfetched without a word.
	for _, keys := range cfg.exchangeKeys() {
		if keys.partial() {
			return Config{}, fmt.Errorf("load config: set all of %s, or none", strings.Join(keys.names, ", "))
		}
	}
	return cfg, nil
}

type exchangeKeys struct {
	names  []string
	values []string
}

func (k exchangeKeys) set() bool {
	return !slices.Contains(k.values, "")
}

func (k exchangeKeys) partial() bool {
	return !k.set() && slices.ContainsFunc(k.values, func(v string) bool { return v != "" })
}

func (c Config) exchangeKeys() map[string]exchangeKeys {
	return map[string]exchangeKeys{
		"max": {
			names:  []string{"BACKEND_MAX_ACCESS_KEY", "BACKEND_MAX_SECRET_KEY"},
			values: []string{c.MaxAccessKey, c.MaxSecretKey},
		},
		"binance": {
			names:  []string{"BACKEND_BINANCE_API_KEY", "BACKEND_BINANCE_API_SECRET"},
			values: []string{c.BinanceAPIKey, c.BinanceAPISecret},
		},
		"bitget": {
			names:  []string{"BACKEND_BITGET_API_KEY", "BACKEND_BITGET_API_SECRET", "BACKEND_BITGET_PASSPHRASE"},
			values: []string{c.BitgetAPIKey, c.BitgetAPISecret, c.BitgetPassphrase},
		},
	}
}

// ExchangeEnabled reports whether all of an exchange's keys are set (ADR 0018).
func (c Config) ExchangeEnabled(institution string) bool {
	keys, ok := c.exchangeKeys()[institution]
	return ok && keys.set()
}

// AccessEnabled reports whether requests must carry a Cloudflare Access token.
func (c Config) AccessEnabled() bool {
	return c.AccessTeamDomain != ""
}

func (c Config) MaxUploadBytes() int64 {
	return int64(c.MaxUploadMB) << 20
}
