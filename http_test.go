package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestHTTPHandlersWithPrefixAndEndToEndFlow(t *testing.T) {
	now := time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC)
	store := newTestStore(t, func() time.Time { return now })
	tgClient := &fakeTelegramClient{}
	bot := NewTelegramBot(Config{TelegramChannelID: -100123}, store, tgClient)
	issuer := &fakeIssuer{}

	cfg := Config{
		HTTPPathPrefix:     "/app",
		TelegramChannelID:  -100123,
		TokenTTL:           24 * time.Hour,
		TelegramUseWebhook: false,
	}
	app := NewApp(cfg, store, issuer, bot)
	server := httptest.NewServer(app.Handler())
	defer server.Close()

	resp, err := http.Get(server.URL + "/app/")
	if err != nil {
		t.Fatalf("GET /app/: %v", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read home response: %v", err)
	}
	body := string(bodyBytes)
	if regexp.MustCompile(`/verify [A-Za-z0-9]{32}`).MatchString(body) {
		t.Fatalf("did not expect a generated verification command on initial page load")
	}
	if strings.Contains(body, "Start Verification") {
		t.Fatalf("did not expect a start verification button in page body")
	}

	startCode, startPayload := postJSON(t, server.URL+"/app/api/verification/start", map[string]any{})
	if startCode != http.StatusOK {
		t.Fatalf("start endpoint returned %d payload=%v", startCode, startPayload)
	}
	token, _ := startPayload["token"].(string)
	sessionToken, _ := startPayload["session_token"].(string)
	command, _ := startPayload["command"].(string)
	if token == "" || sessionToken == "" || command == "" {
		t.Fatalf("expected token, session_token and command from start endpoint, got %v", startPayload)
	}
	if !strings.Contains(command, token) {
		t.Fatalf("expected command to include token, got %q", command)
	}

	statusCode, statusPayload := postJSON(t, server.URL+"/app/api/verification/status", tokenRequest{Token: token, SessionToken: sessionToken})
	if statusCode != http.StatusOK {
		t.Fatalf("status endpoint returned %d", statusCode)
	}
	if statusPayload["status"] != string(StatusPending) {
		t.Fatalf("expected pending status, got %v", statusPayload["status"])
	}

	if err := bot.ProcessUpdate(context.Background(), TelegramUpdate{
		UpdateID: 1,
		ChannelPost: &TelegramMessage{
			MessageID: 7,
			Chat:      TelegramChat{ID: -100123},
			Text:      "/verify " + token,
		},
	}); err != nil {
		t.Fatalf("ProcessUpdate: %v", err)
	}

	statusCode, statusPayload = postJSON(t, server.URL+"/app/api/verification/status", tokenRequest{Token: token, SessionToken: sessionToken})
	if statusCode != http.StatusOK {
		t.Fatalf("status endpoint after verification returned %d", statusCode)
	}
	if statusPayload["status"] != string(StatusVerified) {
		t.Fatalf("expected verified status, got %v", statusPayload["status"])
	}

	keyCode, keyPayload := postJSON(t, server.URL+"/app/api/keys", tokenRequest{Token: token, SessionToken: sessionToken})
	if keyCode != http.StatusOK {
		t.Fatalf("key endpoint returned %d", keyCode)
	}
	if keyPayload["key"] == "" {
		t.Fatalf("expected issued key")
	}
	if issuer.calls != 1 {
		t.Fatalf("expected issuer to be called once, got %d", issuer.calls)
	}

	keyCode, keyPayload = postJSON(t, server.URL+"/app/api/keys", tokenRequest{Token: token, SessionToken: sessionToken})
	if keyCode != http.StatusConflict {
		t.Fatalf("expected second key request conflict, got %d payload=%v", keyCode, keyPayload)
	}
}

func TestTelegramWebhookRejectsInvalidSecret(t *testing.T) {
	now := time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC)
	store := newTestStore(t, func() time.Time { return now })
	bot := NewTelegramBot(Config{TelegramChannelID: -100123}, store, &fakeTelegramClient{})
	app := NewApp(Config{
		TelegramUseWebhook:    true,
		TelegramWebhookSecret: "expected-secret",
		TelegramChannelID:     -100123,
	}, store, &fakeIssuer{}, bot)

	request := httptest.NewRequest(http.MethodPost, defaultWebhookPath, bytes.NewReader([]byte(`{"update_id":1}`)))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	app.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", response.Code)
	}
}

func postJSON(t *testing.T, url string, payload any) (int, map[string]any) {
	t.Helper()

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()

	var decoded map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp.StatusCode, decoded
}
