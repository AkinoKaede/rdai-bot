package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg, err := LoadConfig(os.Args[1:], os.Getenv)
	if err != nil {
		return err
	}

	db, err := sql.Open("sqlite3", cfg.SQLitePath)
	if err != nil {
		return fmt.Errorf("open sqlite database: %w", err)
	}
	defer db.Close()

	store := NewStore(db, time.Now)
	if err := store.Init(context.Background()); err != nil {
		return fmt.Errorf("initialize database: %w", err)
	}

	issuer, err := NewAxonHubKeyIssuer(cfg)
	if err != nil {
		return err
	}

	tgClient := NewTelegramHTTPClient(cfg.TelegramBotToken)
	bot := NewTelegramBot(cfg, store, tgClient)
	app := NewApp(cfg, store, issuer, bot)

	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           app.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 2)

	if cfg.TelegramUseWebhook {
		if err := bot.ConfigureWebhook(rootCtx); err != nil {
			return err
		}
	} else {
		go func() {
			errCh <- bot.RunPolling(rootCtx)
		}()
	}

	go func() {
		log.Printf("serving HTTP on %s with path prefix %q", cfg.HTTPAddr, cfg.HTTPPathPrefix)
		errCh <- server.ListenAndServe()
	}()

	select {
	case <-rootCtx.Done():
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, context.Canceled) {
			stop()
			return err
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("shutdown http server: %w", err)
	}

	return nil
}
