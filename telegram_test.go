package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeTelegramClient struct {
	replies       []string
	updateErrors  []error
	getUpdatesCnt int
	onGetUpdates  func()
}

func (f *fakeTelegramClient) GetUpdates(context.Context, int, int) ([]TelegramUpdate, error) {
	f.getUpdatesCnt++
	if f.onGetUpdates != nil {
		f.onGetUpdates()
	}
	if len(f.updateErrors) > 0 {
		err := f.updateErrors[0]
		f.updateErrors = f.updateErrors[1:]
		return nil, err
	}
	return nil, nil
}

func (f *fakeTelegramClient) SendReply(_ context.Context, _ int64, _ int, text string) error {
	f.replies = append(f.replies, text)
	return nil
}

func (f *fakeTelegramClient) SetWebhook(context.Context, string, string) error {
	return nil
}

func (f *fakeTelegramClient) DeleteWebhook(context.Context) error {
	return nil
}

func TestParseVerifyCommand(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		token   string
		matched bool
		wantErr bool
	}{
		{name: "valid", text: "/verify abc123", token: "abc123", matched: true},
		{name: "valid with bot username", text: "/verify@rdai_bot abc123", token: "abc123", matched: true},
		{name: "missing token", text: "/verify", matched: true, wantErr: true},
		{name: "malformed", text: "/verify abc 123", matched: true, wantErr: true},
		{name: "ignore other command", text: "/start", matched: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, matched, err := parseVerifyCommand(tt.text)
			if matched != tt.matched {
				t.Fatalf("matched=%v want %v", matched, tt.matched)
			}
			if token != tt.token {
				t.Fatalf("token=%q want %q", token, tt.token)
			}
			if (err != nil) != tt.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tt.wantErr)
			}
		})
	}
}

func TestTelegramBotRepliesToSuccessfulVerification(t *testing.T) {
	now := time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC)
	store := newTestStore(t, func() time.Time { return now })
	record, err := store.CreateVerification(context.Background(), 24*time.Hour)
	if err != nil {
		t.Fatalf("CreateVerification: %v", err)
	}

	client := &fakeTelegramClient{}
	bot := NewTelegramBot(Config{TelegramChannelID: -100123}, store, client)

	update := TelegramUpdate{
		UpdateID: 1,
		ChannelPost: &TelegramMessage{
			MessageID: 10,
			Chat:      TelegramChat{ID: -100123},
			Text:      "/verify " + record.Token,
		},
	}

	if err := bot.ProcessUpdate(context.Background(), update); err != nil {
		t.Fatalf("ProcessUpdate: %v", err)
	}
	if len(client.replies) != 1 {
		t.Fatalf("expected 1 reply, got %d", len(client.replies))
	}
	if client.replies[0] == "" {
		t.Fatal("expected non-empty reply")
	}
}

func TestTelegramBotPollingRetriesOnTimeout(t *testing.T) {
	oldDelay := telegramPollRetryDelay
	telegramPollRetryDelay = 0
	defer func() {
		telegramPollRetryDelay = oldDelay
	}()

	ctx, cancel := context.WithCancel(context.Background())
	client := &fakeTelegramClient{
		updateErrors: []error{context.DeadlineExceeded},
		onGetUpdates: func() {},
	}
	client.onGetUpdates = func() {
		if client.getUpdatesCnt >= 2 {
			cancel()
		}
	}

	bot := NewTelegramBot(Config{}, nil, client)
	err := bot.RunPolling(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
	if client.getUpdatesCnt < 2 {
		t.Fatalf("expected polling to retry, got %d calls", client.getUpdatesCnt)
	}
}
