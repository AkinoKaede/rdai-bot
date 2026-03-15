package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type fakeIssuer struct {
	calls int
	key   *IssuedKey
	err   error
}

func (f *fakeIssuer) CreateAPIKey(_ context.Context, name string) (*IssuedKey, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	if f.key != nil {
		return &IssuedKey{
			Name:   name,
			Key:    f.key.Key,
			Scopes: append([]string(nil), f.key.Scopes...),
		}, nil
	}
	return &IssuedKey{Name: name, Key: "issued-key", Scopes: []string{"read_channels", "write_requests"}}, nil
}

func TestStoreTokenLifecycle(t *testing.T) {
	now := time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC)
	store := newTestStore(t, func() time.Time { return now })

	record, err := store.CreateVerification(context.Background(), 24*time.Hour)
	if err != nil {
		t.Fatalf("CreateVerification: %v", err)
	}
	if record.Status != StatusPending {
		t.Fatalf("expected pending status, got %s", record.Status)
	}
	if len(record.Token) != verificationTokenLength {
		t.Fatalf("expected token length %d, got %d", verificationTokenLength, len(record.Token))
	}
	if !regexp.MustCompile(`^[A-Za-z0-9]{32}$`).MatchString(record.Token) {
		t.Fatalf("token has unexpected format: %q", record.Token)
	}
	if len(record.SessionToken) != verificationTokenLength {
		t.Fatalf("expected session token length %d, got %d", verificationTokenLength, len(record.SessionToken))
	}

	verified, err := store.MarkVerified(context.Background(), record.Token)
	if err != nil {
		t.Fatalf("MarkVerified: %v", err)
	}
	if verified.Status != StatusVerified {
		t.Fatalf("expected verified status, got %s", verified.Status)
	}

	issuer := &fakeIssuer{}
	issued, err := store.IssueKey(context.Background(), record.Token, record.SessionToken, issuer)
	if err != nil {
		t.Fatalf("IssueKey: %v", err)
	}
	if issued.Key == "" {
		t.Fatal("expected issued key")
	}
	if issuer.calls != 1 {
		t.Fatalf("expected issuer to be called once, got %d", issuer.calls)
	}

	issuedAgain, err := store.IssueKey(context.Background(), record.Token, record.SessionToken, issuer)
	if err != ErrTokenIssued {
		t.Fatalf("expected ErrTokenIssued, got %v", err)
	}
	if issuedAgain != nil {
		t.Fatalf("expected no issued key on second retrieval")
	}
	if issuer.calls != 1 {
		t.Fatalf("expected issuer to still be called once, got %d", issuer.calls)
	}
}

func TestStoreExpiresPendingToken(t *testing.T) {
	now := time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC)
	store := newTestStore(t, func() time.Time { return now })

	record, err := store.CreateVerification(context.Background(), time.Hour)
	if err != nil {
		t.Fatalf("CreateVerification: %v", err)
	}

	store.now = func() time.Time { return now.Add(2 * time.Hour) }

	loaded, err := store.GetByToken(context.Background(), record.Token)
	if err != nil {
		t.Fatalf("GetByToken: %v", err)
	}
	if loaded.Status != StatusExpired {
		t.Fatalf("expected expired status, got %s", loaded.Status)
	}

	if _, err := store.IssueKey(context.Background(), record.Token, record.SessionToken, &fakeIssuer{}); err != ErrTokenExpired {
		t.Fatalf("expected ErrTokenExpired, got %v", err)
	}
}

func TestStoreExpiresTokenAtExactBoundary(t *testing.T) {
	now := time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC)
	store := newTestStore(t, func() time.Time { return now })

	record, err := store.CreateVerification(context.Background(), time.Hour)
	if err != nil {
		t.Fatalf("CreateVerification: %v", err)
	}

	store.now = func() time.Time { return record.ExpiresAt }

	loaded, err := store.GetByToken(context.Background(), record.Token)
	if err != nil {
		t.Fatalf("GetByToken: %v", err)
	}
	if loaded.Status != StatusExpired {
		t.Fatalf("expected expired status at boundary, got %s", loaded.Status)
	}

	if _, err := store.MarkVerified(context.Background(), record.Token); err != ErrTokenExpired {
		t.Fatalf("expected ErrTokenExpired from MarkVerified at boundary, got %v", err)
	}

	if _, err := store.IssueKey(context.Background(), record.Token, record.SessionToken, &fakeIssuer{}); err != ErrTokenExpired {
		t.Fatalf("expected ErrTokenExpired from IssueKey at boundary, got %v", err)
	}
}

func TestStoreRejectsWrongSessionToken(t *testing.T) {
	now := time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC)
	store := newTestStore(t, func() time.Time { return now })

	record, err := store.CreateVerification(context.Background(), 24*time.Hour)
	if err != nil {
		t.Fatalf("CreateVerification: %v", err)
	}

	if _, err := store.GetBySession(context.Background(), record.Token, "wrong-session-token"); err != ErrSessionInvalid {
		t.Fatalf("expected ErrSessionInvalid, got %v", err)
	}
}

func newTestStore(t *testing.T, now func() time.Time) *Store {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	store := NewStore(db, now)
	if err := store.Init(context.Background()); err != nil {
		t.Fatalf("init store: %v", err)
	}
	return store
}
