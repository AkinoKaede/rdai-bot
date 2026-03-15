package main

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"
)

type VerificationStatus string

const (
	StatusPending  VerificationStatus = "pending"
	StatusVerified VerificationStatus = "verified"
	StatusIssued   VerificationStatus = "issued"
	StatusExpired  VerificationStatus = "expired"
)

var (
	ErrTokenNotFound  = errors.New("verification token not found")
	ErrTokenPending   = errors.New("verification token is still pending")
	ErrTokenExpired   = errors.New("verification token has expired")
	ErrTokenIssued    = errors.New("verification token already issued")
	ErrTokenUsed      = errors.New("verification token already verified or used")
	ErrTokenInvalid   = errors.New("verification token is invalid")
	ErrSessionInvalid = errors.New("verification session is invalid")
)

type VerificationRecord struct {
	ID             int64
	Token          string
	SessionToken   string
	Status         VerificationStatus
	CreatedAt      time.Time
	ExpiresAt      time.Time
	VerifiedAt     sql.NullTime
	IssuedAt       sql.NullTime
	DeliveredAt    sql.NullTime
	AxonHubKeyName sql.NullString
	Scopes         []string
}

type Store struct {
	db  *sql.DB
	now func() time.Time
	mu  sync.Mutex
}

func NewStore(db *sql.DB, now func() time.Time) *Store {
	return &Store{db: db, now: now}
}

func (s *Store) Init(ctx context.Context) error {
	const schema = `
CREATE TABLE IF NOT EXISTS verification_records (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	token TEXT NOT NULL UNIQUE,
	session_token TEXT,
	status TEXT NOT NULL,
	created_at TIMESTAMP NOT NULL,
	expires_at TIMESTAMP NOT NULL,
	verified_at TIMESTAMP,
	issued_at TIMESTAMP,
	delivered_at TIMESTAMP,
	axonhub_key_name TEXT,
	axonhub_scopes TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_verification_records_token ON verification_records(token);
`
	_, err := s.db.ExecContext(ctx, schema)
	return err
}

func (s *Store) CreateVerification(ctx context.Context, ttl time.Duration) (*VerificationRecord, error) {
	now := s.now().UTC()
	expiresAt := now.Add(ttl)

	for range 5 {
		token, err := newToken()
		if err != nil {
			return nil, err
		}
		sessionToken, err := newSessionToken()
		if err != nil {
			return nil, err
		}

		result, err := s.db.ExecContext(
			ctx,
			`INSERT INTO verification_records (token, session_token, status, created_at, expires_at) VALUES (?, ?, ?, ?, ?)`,
			token,
			sessionToken,
			StatusPending,
			now,
			expiresAt,
		)
		if err != nil {
			continue
		}

		id, err := result.LastInsertId()
		if err != nil {
			return nil, fmt.Errorf("fetch inserted verification id: %w", err)
		}

		return &VerificationRecord{
			ID:           id,
			Token:        token,
			SessionToken: sessionToken,
			Status:       StatusPending,
			CreatedAt:    now,
			ExpiresAt:    expiresAt,
		}, nil
	}

	return nil, fmt.Errorf("create verification token: too many collisions")
}

func (s *Store) GetByToken(ctx context.Context, token string) (*VerificationRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.getByTokenLocked(ctx, token)
}

func (s *Store) getByTokenLocked(ctx context.Context, token string) (*VerificationRecord, error) {
	record, err := s.loadByToken(ctx, token)
	if err != nil {
		return nil, err
	}

	if record.Status == StatusPending && !s.now().UTC().Before(record.ExpiresAt) {
		if err := s.expireToken(ctx, record.Token); err != nil {
			return nil, err
		}
		record.Status = StatusExpired
	}

	return record, nil
}

func (s *Store) GetBySession(ctx context.Context, token string, sessionToken string) (*VerificationRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	record, err := s.getByTokenLocked(ctx, token)
	if err != nil {
		return nil, err
	}
	if !sessionTokenMatches(record.SessionToken, sessionToken) {
		return nil, ErrSessionInvalid
	}
	return record, nil
}

func (s *Store) MarkVerified(ctx context.Context, token string) (*VerificationRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	record, err := s.loadByToken(ctx, token)
	if err != nil {
		return nil, err
	}

	now := s.now().UTC()
	if record.Status == StatusPending && !now.Before(record.ExpiresAt) {
		if err := s.expireToken(ctx, token); err != nil {
			return nil, err
		}
		record.Status = StatusExpired
		return record, ErrTokenExpired
	}

	switch record.Status {
	case StatusPending:
		if _, err := s.db.ExecContext(
			ctx,
			`UPDATE verification_records SET status = ?, verified_at = ? WHERE token = ?`,
			StatusVerified,
			now,
			token,
		); err != nil {
			return nil, fmt.Errorf("mark token verified: %w", err)
		}
		record.Status = StatusVerified
		record.VerifiedAt = sql.NullTime{Time: now, Valid: true}
		return record, nil
	case StatusVerified:
		return record, ErrTokenUsed
	case StatusIssued:
		return record, ErrTokenIssued
	case StatusExpired:
		return record, ErrTokenExpired
	default:
		return record, ErrTokenInvalid
	}
}

func (s *Store) IssueKey(ctx context.Context, token string, sessionToken string, issuer KeyIssuer) (*IssuedKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	record, err := s.loadByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if !sessionTokenMatches(record.SessionToken, sessionToken) {
		return nil, ErrSessionInvalid
	}

	now := s.now().UTC()
	if record.Status == StatusPending && !now.Before(record.ExpiresAt) {
		if err := s.expireToken(ctx, token); err != nil {
			return nil, err
		}
		return nil, ErrTokenExpired
	}

	switch record.Status {
	case StatusPending:
		return nil, ErrTokenPending
	case StatusExpired:
		return nil, ErrTokenExpired
	case StatusIssued:
		return nil, ErrTokenIssued
	case StatusVerified:
		keyName := fmt.Sprintf("%s%d", defaultKeyNamePrefix, record.ID)
		issued, err := issuer.CreateAPIKey(ctx, keyName)
		if err != nil {
			return nil, err
		}
		if _, err := s.db.ExecContext(
			ctx,
			`UPDATE verification_records
			 SET status = ?, issued_at = ?, delivered_at = ?, axonhub_key_name = ?, axonhub_scopes = ?
			 WHERE token = ?`,
			StatusIssued,
			now,
			now,
			issued.Name,
			joinScopes(issued.Scopes),
			token,
		); err != nil {
			return nil, fmt.Errorf("persist issued key: %w", err)
		}
		return issued, nil
	default:
		return nil, ErrTokenInvalid
	}
}

func (s *Store) expireToken(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(
		ctx,
		`UPDATE verification_records SET status = ? WHERE token = ? AND status = ?`,
		StatusExpired,
		token,
		StatusPending,
	)
	if err != nil {
		return fmt.Errorf("expire token: %w", err)
	}
	return nil
}

func (s *Store) loadByToken(ctx context.Context, token string) (*VerificationRecord, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT id, token, session_token, status, created_at, expires_at, verified_at, issued_at, delivered_at, axonhub_key_name, axonhub_scopes
		 FROM verification_records
		 WHERE token = ?`,
		token,
	)

	var record VerificationRecord
	var status string
	var scopes string
	if err := row.Scan(
		&record.ID,
		&record.Token,
		&record.SessionToken,
		&status,
		&record.CreatedAt,
		&record.ExpiresAt,
		&record.VerifiedAt,
		&record.IssuedAt,
		&record.DeliveredAt,
		&record.AxonHubKeyName,
		&scopes,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrTokenNotFound
		}
		return nil, fmt.Errorf("load verification token: %w", err)
	}
	record.Status = VerificationStatus(status)
	record.Scopes = splitScopes(scopes)
	return &record, nil
}

func sessionTokenMatches(expected string, provided string) bool {
	if expected == "" || provided == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(provided)) == 1
}
