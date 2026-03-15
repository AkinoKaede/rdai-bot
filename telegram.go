package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var telegramPollRetryDelay = time.Second

type TelegramBot struct {
	cfg    Config
	store  *Store
	client TelegramClient
}

type TelegramClient interface {
	GetUpdates(ctx context.Context, offset int, timeoutSeconds int) ([]TelegramUpdate, error)
	SendReply(ctx context.Context, chatID int64, messageID int, text string) error
	SetWebhook(ctx context.Context, webhookURL string, secretToken string) error
	DeleteWebhook(ctx context.Context) error
}

type TelegramHTTPClient struct {
	baseURL string
	client  *http.Client
}

type TelegramUpdate struct {
	UpdateID          int              `json:"update_id"`
	ChannelPost       *TelegramMessage `json:"channel_post"`
	EditedChannelPost *TelegramMessage `json:"edited_channel_post"`
}

type TelegramMessage struct {
	MessageID int          `json:"message_id"`
	Chat      TelegramChat `json:"chat"`
	Text      string       `json:"text"`
}

type TelegramChat struct {
	ID int64 `json:"id"`
}

func NewTelegramBot(cfg Config, store *Store, client TelegramClient) *TelegramBot {
	return &TelegramBot{
		cfg:    cfg,
		store:  store,
		client: client,
	}
}

func NewTelegramHTTPClient(botToken string) *TelegramHTTPClient {
	return &TelegramHTTPClient{
		baseURL: "https://api.telegram.org/bot" + botToken,
		client: &http.Client{
			Timeout: 45 * time.Second,
		},
	}
}

func (b *TelegramBot) ConfigureWebhook(ctx context.Context) error {
	return b.client.SetWebhook(
		ctx,
		strings.TrimRight(b.cfg.TelegramWebhookURL, "/")+defaultWebhookPath,
		b.cfg.TelegramWebhookSecret,
	)
}

func (b *TelegramBot) RunPolling(ctx context.Context) error {
	if err := b.client.DeleteWebhook(ctx); err != nil {
		return fmt.Errorf("disable telegram webhook: %w", err)
	}

	offset := 0
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		updates, err := b.client.GetUpdates(ctx, offset, 30)
		if err != nil {
			if isRetryableTelegramPollError(err) {
				if err := sleepWithContext(ctx, telegramPollRetryDelay); err != nil {
					return err
				}
				continue
			}
			return fmt.Errorf("poll telegram updates: %w", err)
		}

		for _, update := range updates {
			offset = update.UpdateID + 1
			if err := b.ProcessUpdate(ctx, update); err != nil {
				return err
			}
		}
	}
}

func isRetryableTelegramPollError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func sleepWithContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (b *TelegramBot) ProcessUpdate(ctx context.Context, update TelegramUpdate) error {
	message := update.ChannelPost
	if message == nil {
		return nil
	}
	if message.Chat.ID != b.cfg.TelegramChannelID {
		return nil
	}

	token, matched, err := parseVerifyCommand(message.Text)
	if !matched {
		return nil
	}
	if err != nil {
		return b.client.SendReply(ctx, message.Chat.ID, message.MessageID, "Invalid verification command. Use /verify <token>.")
	}

	_, err = b.store.MarkVerified(ctx, token)
	switch {
	case err == nil:
		return b.client.SendReply(ctx, message.Chat.ID, message.MessageID, "Verification successful. Return to the website and request your API key.")
	case errors.Is(err, ErrTokenExpired):
		return b.client.SendReply(ctx, message.Chat.ID, message.MessageID, "Verification token has expired. Load the page again to get a new token.")
	case errors.Is(err, ErrTokenIssued), errors.Is(err, ErrTokenUsed):
		return b.client.SendReply(ctx, message.Chat.ID, message.MessageID, "Verification token has already been used.")
	case errors.Is(err, ErrTokenNotFound):
		return b.client.SendReply(ctx, message.Chat.ID, message.MessageID, "Verification token was not found.")
	default:
		return err
	}
}

func parseVerifyCommand(text string) (token string, matched bool, err error) {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) == 0 {
		return "", false, nil
	}

	command := fields[0]
	if !strings.HasPrefix(command, "/verify") {
		return "", false, nil
	}

	base := strings.SplitN(command, "@", 2)[0]
	if base != "/verify" {
		return "", false, nil
	}
	if len(fields) != 2 {
		return "", true, ErrTokenInvalid
	}
	return fields[1], true, nil
}

func (c *TelegramHTTPClient) GetUpdates(ctx context.Context, offset int, timeoutSeconds int) ([]TelegramUpdate, error) {
	values := url.Values{}
	values.Set("offset", fmt.Sprintf("%d", offset))
	values.Set("timeout", fmt.Sprintf("%d", timeoutSeconds))

	var response struct {
		OK     bool             `json:"ok"`
		Result []TelegramUpdate `json:"result"`
	}
	if err := c.postForm(ctx, "/getUpdates", values, &response); err != nil {
		return nil, err
	}
	return response.Result, nil
}

func (c *TelegramHTTPClient) SendReply(ctx context.Context, chatID int64, messageID int, text string) error {
	values := url.Values{}
	values.Set("chat_id", fmt.Sprintf("%d", chatID))
	values.Set("reply_to_message_id", fmt.Sprintf("%d", messageID))
	values.Set("text", text)
	return c.postForm(ctx, "/sendMessage", values, nil)
}

func (c *TelegramHTTPClient) SetWebhook(ctx context.Context, webhookURL string, secretToken string) error {
	values := url.Values{}
	values.Set("url", webhookURL)
	values.Set("secret_token", secretToken)
	return c.postForm(ctx, "/setWebhook", values, nil)
}

func (c *TelegramHTTPClient) DeleteWebhook(ctx context.Context) error {
	return c.postForm(ctx, "/deleteWebhook", url.Values{}, nil)
}

func (c *TelegramHTTPClient) postForm(ctx context.Context, path string, values url.Values, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, strings.NewReader(values.Encode()))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := c.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return fmt.Errorf("read telegram response: %w", err)
	}
	if response.StatusCode >= 300 {
		return fmt.Errorf("telegram api returned %s: %s", response.Status, bytes.TrimSpace(body))
	}

	if target == nil {
		return nil
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("decode telegram response: %w", err)
	}
	return nil
}
