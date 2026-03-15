package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"html/template"
	"log"
	"net/http"
	"strings"
)

type App struct {
	cfg      Config
	store    *Store
	issuer   KeyIssuer
	bot      *TelegramBot
	template *template.Template
}

type pageData struct {
	StartEndpoint  string
	StatusEndpoint string
	KeyEndpoint    string
}

type tokenRequest struct {
	Token        string `json:"token"`
	SessionToken string `json:"session_token"`
}

type startResponse struct {
	Token        string `json:"token"`
	SessionToken string `json:"session_token"`
	Command      string `json:"command"`
}

type statusResponse struct {
	Status  VerificationStatus `json:"status"`
	Message string             `json:"message"`
}

type errorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

func NewApp(cfg Config, store *Store, issuer KeyIssuer, bot *TelegramBot) *App {
	return &App{
		cfg:      cfg,
		store:    store,
		issuer:   issuer,
		bot:      bot,
		template: mustParseTemplates(),
	}
}

func (a *App) Handler() http.Handler {
	appMux := http.NewServeMux()
	appMux.HandleFunc("/", a.handleHome)
	appMux.HandleFunc("/robots.txt", a.handleRobots)
	appMux.HandleFunc("/api/verification/start", a.handleVerificationStart)
	appMux.HandleFunc("/api/verification/status", a.handleVerificationStatus)
	appMux.HandleFunc("/api/keys", a.handleCreateKey)

	rootMux := http.NewServeMux()
	rootMux.HandleFunc(defaultWebhookPath, a.handleTelegramWebhook)

	if a.cfg.HTTPPathPrefix == "" {
		rootMux.Handle("/", appMux)
		return a.loggingMiddleware(rootMux)
	}

	rootMux.Handle(a.cfg.HTTPPathPrefix+"/", http.StripPrefix(a.cfg.HTTPPathPrefix, appMux))
	rootMux.HandleFunc("GET "+a.cfg.HTTPPathPrefix, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, a.cfg.HTTPPathPrefix+"/", http.StatusTemporaryRedirect)
	})
	return a.loggingMiddleware(rootMux)
}

func (a *App) handleHome(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	data := pageData{
		StartEndpoint:  pathWithPrefix(a.cfg.HTTPPathPrefix, "/api/verification/start"),
		StatusEndpoint: pathWithPrefix(a.cfg.HTTPPathPrefix, "/api/verification/status"),
		KeyEndpoint:    pathWithPrefix(a.cfg.HTTPPathPrefix, "/api/keys"),
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := a.template.Execute(w, data); err != nil {
		log.Printf("render index page: %v", err)
	}
}

func (a *App) handleRobots(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || r.URL.Path != "/robots.txt" {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("User-agent: *\nDisallow: /\n"))
}

func (a *App) handleVerificationStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}

	record, err := a.store.CreateVerification(r.Context(), a.cfg.TokenTTL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create_token_failed", "Failed to create verification token.")
		return
	}

	writeJSON(w, http.StatusOK, startResponse{
		Token:        record.Token,
		SessionToken: record.SessionToken,
		Command:      "/verify " + record.Token,
	})
}

func (a *App) handleVerificationStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}

	var req tokenRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON request.")
		return
	}

	record, err := a.store.GetBySession(r.Context(), strings.TrimSpace(req.Token), strings.TrimSpace(req.SessionToken))
	if err != nil {
		writeStoreError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, statusResponse{
		Status:  record.Status,
		Message: statusMessage(record.Status),
	})
}

func (a *App) handleCreateKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}

	var req tokenRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON request.")
		return
	}

	issued, err := a.store.IssueKey(r.Context(), strings.TrimSpace(req.Token), strings.TrimSpace(req.SessionToken), a.issuer)
	if err != nil {
		switch {
		case errors.Is(err, ErrTokenPending):
			writeError(w, http.StatusConflict, "not_verified", "Verification has not completed yet.")
		case errors.Is(err, ErrTokenExpired):
			writeError(w, http.StatusGone, "token_expired", "Verification token has expired.")
		case errors.Is(err, ErrTokenIssued):
			writeError(w, http.StatusConflict, "key_already_issued", "API key has already been retrieved for this verification token.")
		case errors.Is(err, ErrTokenNotFound):
			writeError(w, http.StatusNotFound, "token_not_found", "Verification token was not found.")
		case errors.Is(err, ErrSessionInvalid):
			writeError(w, http.StatusNotFound, "session_not_found", "Verification session was not found.")
		default:
			writeError(w, http.StatusBadGateway, "key_creation_failed", "Failed to create API key.")
		}
		return
	}

	writeJSON(w, http.StatusOK, issued)
}

func (a *App) handleTelegramWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}

	if !a.cfg.TelegramUseWebhook {
		writeError(w, http.StatusNotFound, "webhook_disabled", "Webhook mode is disabled.")
		return
	}
	if subtle.ConstantTimeCompare(
		[]byte(r.Header.Get("X-Telegram-Bot-Api-Secret-Token")),
		[]byte(a.cfg.TelegramWebhookSecret),
	) != 1 {
		writeError(w, http.StatusUnauthorized, "invalid_telegram_secret", "Invalid Telegram webhook secret.")
		return
	}

	var update TelegramUpdate
	if err := decodeJSON(r, &update); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid Telegram update payload.")
		return
	}

	if err := a.bot.ProcessUpdate(context.Background(), update); err != nil {
		log.Printf("process telegram webhook update: %v", err)
		writeError(w, http.StatusBadGateway, "telegram_update_failed", "Failed to process Telegram update.")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *App) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

func pathWithPrefix(prefix, path string) string {
	if prefix == "" {
		return path
	}
	if path == "/" {
		return prefix + "/"
	}
	return prefix + path
}

func statusMessage(status VerificationStatus) string {
	switch status {
	case StatusPending:
		return "Verification is still pending."
	case StatusVerified:
		return "Verification is complete. You can request an API key."
	case StatusIssued:
		return "API key has already been issued for this verification token."
	case StatusExpired:
		return "Verification token has expired."
	default:
		return "Verification status is unknown."
	}
}

func decodeJSON(r *http.Request, target any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorResponse{
		Error:   code,
		Message: message,
	})
}

func writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrTokenNotFound):
		writeError(w, http.StatusNotFound, "token_not_found", "Verification token was not found.")
	case errors.Is(err, ErrTokenExpired):
		writeError(w, http.StatusGone, "token_expired", "Verification token has expired.")
	case errors.Is(err, ErrSessionInvalid):
		writeError(w, http.StatusNotFound, "session_not_found", "Verification session was not found.")
	default:
		writeError(w, http.StatusInternalServerError, "storage_error", "Failed to load verification status.")
	}
}
