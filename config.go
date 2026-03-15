package main

import (
	"errors"
	"flag"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	defaultHTTPAddr      = "0.0.0.0:8080"
	defaultTokenTTL      = 24 * time.Hour
	defaultWebhookPath   = "/telegram/webhook"
	defaultKeyNamePrefix = "rdai-"
)

type Config struct {
	HTTPAddr              string
	HTTPPathPrefix        string
	SQLitePath            string
	AxonHubEndpoint       string
	AxonHubAPIKey         string
	TelegramBotToken      string
	TelegramChannelID     int64
	TelegramUseWebhook    bool
	TelegramWebhookURL    string
	TelegramWebhookSecret string
	TokenTTL              time.Duration
	RateLimit             float64
	RateBurst             int
}

func LoadConfig(args []string, getenv func(string) string) (Config, error) {
	fs := flag.NewFlagSet("rdai-bot", flag.ContinueOnError)

	sqliteDefault := firstNonEmpty(getenv("SQLITE_PATH"), "/data/rdai-bot.db")
	httpAddrDefault := firstNonEmpty(getenv("HTTP_ADDR"), defaultHTTPAddr)
	httpPrefixDefault := normalizeHTTPPathPrefix(getenv("HTTP_PATH_PREFIX"))
	ttlDefault := firstNonEmpty(getenv("TOKEN_TTL"), defaultTokenTTL.String())

	sqlitePath := fs.String("sqlite-path", sqliteDefault, "SQLite database path")
	httpAddr := fs.String("http-addr", httpAddrDefault, "HTTP listen address")
	httpPathPrefix := fs.String("http-path-prefix", httpPrefixDefault, "HTTP path prefix")

	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}

	channelID, err := parseInt64(getenv("TELEGRAM_CHANNEL_ID"))
	if err != nil {
		return Config{}, fmt.Errorf("parse TELEGRAM_CHANNEL_ID: %w", err)
	}

	tokenTTL, err := time.ParseDuration(ttlDefault)
	if err != nil {
		return Config{}, fmt.Errorf("parse TOKEN_TTL: %w", err)
	}

	rateLimit := 10.0
	if v := getenv("RATE_LIMIT"); v != "" {
		rateLimit, err = strconv.ParseFloat(v, 64)
		if err != nil {
			return Config{}, fmt.Errorf("parse RATE_LIMIT: %w", err)
		}
	}

	rateBurst := 20
	if v := getenv("RATE_BURST"); v != "" {
		rateBurst, err = strconv.Atoi(v)
		if err != nil {
			return Config{}, fmt.Errorf("parse RATE_BURST: %w", err)
		}
	}

	cfg := Config{
		HTTPAddr:              *httpAddr,
		HTTPPathPrefix:        normalizeHTTPPathPrefix(*httpPathPrefix),
		SQLitePath:            *sqlitePath,
		AxonHubEndpoint:       firstNonEmpty(getenv("AXONHUB_ENDPOINT"), "http://localhost:8090/openapi/v1/graphql"),
		AxonHubAPIKey:         getenv("AXONHUB_API_KEY"),
		TelegramBotToken:      getenv("TELEGRAM_BOT_TOKEN"),
		TelegramChannelID:     channelID,
		TelegramUseWebhook:    parseBool(getenv("TELEGRAM_USE_WEBHOOK")),
		TelegramWebhookURL:    strings.TrimSpace(getenv("TELEGRAM_WEBHOOK_URL")),
		TelegramWebhookSecret: strings.TrimSpace(getenv("TELEGRAM_WEBHOOK_SECRET")),
		TokenTTL:              tokenTTL,
		RateLimit:             rateLimit,
		RateBurst:             rateBurst,
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func (c Config) Validate() error {
	if c.SQLitePath == "" {
		return errors.New("sqlite path is required")
	}
	if c.AxonHubAPIKey == "" {
		return errors.New("AXONHUB_API_KEY is required")
	}
	if c.AxonHubEndpoint == "" {
		return errors.New("AXONHUB_ENDPOINT is required")
	}
	if c.TelegramBotToken == "" {
		return errors.New("TELEGRAM_BOT_TOKEN is required")
	}
	if c.TelegramChannelID == 0 {
		return errors.New("TELEGRAM_CHANNEL_ID is required")
	}
	if c.TokenTTL <= 0 {
		return errors.New("TOKEN_TTL must be greater than zero")
	}
	if c.TelegramUseWebhook && c.TelegramWebhookURL == "" {
		return errors.New("TELEGRAM_WEBHOOK_URL is required when TELEGRAM_USE_WEBHOOK is enabled")
	}
	if c.TelegramUseWebhook && c.TelegramWebhookSecret == "" {
		return errors.New("TELEGRAM_WEBHOOK_SECRET is required when TELEGRAM_USE_WEBHOOK is enabled")
	}
	return nil
}

func normalizeHTTPPathPrefix(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" || prefix == "/" {
		return ""
	}
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	return strings.TrimRight(prefix, "/")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
