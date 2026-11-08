// Package config reads the backend's settings from BACKEND_* environment variables.
package config

import (
	"errors"
	"fmt"
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
	return cfg, nil
}

// AccessEnabled reports whether requests must carry a Cloudflare Access token.
func (c Config) AccessEnabled() bool {
	return c.AccessTeamDomain != ""
}

func (c Config) MaxUploadBytes() int64 {
	return int64(c.MaxUploadMB) << 20
}
