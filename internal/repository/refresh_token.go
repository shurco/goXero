package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RefreshTokenRepository persists opaque refresh tokens hashed with SHA-256.
// The raw token never leaves the server after issuance; callers authenticate
// by presenting the raw string whose hash matches a live row.
type RefreshTokenRepository struct {
	pool *pgxpool.Pool
}

// RefreshToken is the persisted representation — never contains the raw secret.
type RefreshToken struct {
	TokenID      uuid.UUID
	UserID       uuid.UUID
	IssuedAt     time.Time
	ExpiresAt    time.Time
	RevokedAt    *time.Time
	ReplacedByID *uuid.UUID
	UserAgent    string
	IP           string
}

// Create inserts a new refresh token row, returning its generated id.
// `tokenHash` must be the SHA-256 hex digest of the raw token.
func (r *RefreshTokenRepository) Create(ctx context.Context, userID uuid.UUID, tokenHash string, expiresAt time.Time, userAgent, ip string) (*RefreshToken, error) {
	rt := &RefreshToken{
		UserID:    userID,
		ExpiresAt: expiresAt,
		UserAgent: userAgent,
		IP:        ip,
	}
	err := r.pool.QueryRow(ctx,
		`INSERT INTO refresh_tokens (user_id, token_hash, expires_at, user_agent, ip)
		 VALUES ($1,$2,$3,NULLIF($4,''),NULLIF($5,''))
		 RETURNING token_id, issued_at`,
		userID, tokenHash, expiresAt, userAgent, ip,
	).Scan(&rt.TokenID, &rt.IssuedAt)
	if err != nil {
		return nil, err
	}
	return rt, nil
}

// GetByHash loads the row whose hash matches — including revoked rows, so
// callers can detect reuse of an already-rotated token.
func (r *RefreshTokenRepository) GetByHash(ctx context.Context, tokenHash string) (*RefreshToken, error) {
	rt := &RefreshToken{}
	var ua, ip *string
	err := r.pool.QueryRow(ctx,
		`SELECT token_id, user_id, issued_at, expires_at, revoked_at, replaced_by_id, user_agent, ip
		   FROM refresh_tokens WHERE token_hash = $1`, tokenHash,
	).Scan(&rt.TokenID, &rt.UserID, &rt.IssuedAt, &rt.ExpiresAt, &rt.RevokedAt, &rt.ReplacedByID, &ua, &ip)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if ua != nil {
		rt.UserAgent = *ua
	}
	if ip != nil {
		rt.IP = *ip
	}
	return rt, nil
}

// Rotate atomically revokes `oldID`, stores `newHash` as a fresh token and
// links them via `replaced_by_id`. The resulting row id is returned.
//
// Rotation is safe even under concurrent refresh attempts: a second caller
// racing on the same hash will pass the reuse check (revoked_at IS NULL) on
// at most one side because `UPDATE … WHERE revoked_at IS NULL` is atomic.
func (r *RefreshTokenRepository) Rotate(ctx context.Context, oldID, userID uuid.UUID, newHash string, newExpiresAt time.Time, userAgent, ip string) (*RefreshToken, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// 1. Revoke the old token, but only if it is still live. If no row was
	// updated the old token is already revoked/rotated — caller treats that
	// as reuse and should revoke the whole family.
	cmd, err := tx.Exec(ctx,
		`UPDATE refresh_tokens SET revoked_at = now()
		 WHERE token_id = $1 AND user_id = $2 AND revoked_at IS NULL`,
		oldID, userID)
	if err != nil {
		return nil, err
	}
	if cmd.RowsAffected() == 0 {
		return nil, ErrForbidden
	}

	// 2. Insert the replacement.
	rt := &RefreshToken{UserID: userID, ExpiresAt: newExpiresAt, UserAgent: userAgent, IP: ip}
	err = tx.QueryRow(ctx,
		`INSERT INTO refresh_tokens (user_id, token_hash, expires_at, user_agent, ip)
		 VALUES ($1,$2,$3,NULLIF($4,''),NULLIF($5,''))
		 RETURNING token_id, issued_at`,
		userID, newHash, newExpiresAt, userAgent, ip,
	).Scan(&rt.TokenID, &rt.IssuedAt)
	if err != nil {
		return nil, err
	}

	// 3. Backfill the link so reuse detection can walk the chain.
	if _, err = tx.Exec(ctx,
		`UPDATE refresh_tokens SET replaced_by_id = $1 WHERE token_id = $2`,
		rt.TokenID, oldID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return rt, nil
}

// RevokeByID marks a single token revoked. Idempotent.
func (r *RefreshTokenRepository) RevokeByID(ctx context.Context, tokenID, userID uuid.UUID) error {
	cmd, err := r.pool.Exec(ctx,
		`UPDATE refresh_tokens SET revoked_at = now()
		 WHERE token_id = $1 AND user_id = $2 AND revoked_at IS NULL`,
		tokenID, userID)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		// Either already revoked or wrong user — either way there is nothing
		// more to do. Don't leak which.
		return nil
	}
	return nil
}

// RevokeAllForUser wipes every live refresh token for the given user. Called
// on logout-everywhere flows and on reuse detection.
func (r *RefreshTokenRepository) RevokeAllForUser(ctx context.Context, userID uuid.UUID) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE refresh_tokens SET revoked_at = now()
		 WHERE user_id = $1 AND revoked_at IS NULL`, userID)
	return err
}
