package clicommand

import (
	"context"
	"github.com/BurntSushi/toml"
	skclient "github.com/kombifyio/SpeechKit/pkg/speechkit/x/client"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type fileConfig struct {
	ServerURL string `toml:"server_url"`
	Token     string `toml:"token"`
	Server    struct {
		URL   string `toml:"url"`
		Token string `toml:"token"`
	} `toml:"server"`
}

func withClient(opts *globalOptions, fn func(context.Context, *skclient.Client) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	c, err := newClient(*opts)
	if err != nil {
		return exitError{code: 2, err: err}
	}
	return fn(ctx, c)
}

func newClient(opts globalOptions) (*skclient.Client, error) {
	cfg := loadFileConfig()
	server := firstNonEmpty(opts.server, os.Getenv("SPEECHKIT_SERVER_URL"), cfg.Server.URL, cfg.ServerURL)
	token := firstNonEmpty(opts.token, os.Getenv("SPEECHKIT_TOKEN"), os.Getenv("SPEECHKIT_SERVER_TOKEN"), cfg.Server.Token, cfg.Token)
	return skclient.New(skclient.Options{BaseURL: server, Token: token, UserAgent: "speechkitctl/0.1"})
}

func loadFileConfig() fileConfig {
	var cfg fileConfig
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return cfg
	}
	path := filepath.Join(home, ".config", "speechkit", "config.toml")
	if _, err := os.Stat(path); err != nil {
		return cfg
	}
	_, _ = toml.DecodeFile(path, &cfg)
	return cfg
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
