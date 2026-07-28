package adapter

import (
	"context"
	"errors"
	"time"

	"AgentScope/internal/auth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SessionRepository struct{ pool *pgxpool.Pool }

func NewSessionRepository(pool *pgxpool.Pool) *SessionRepository {
	return &SessionRepository{pool: pool}
}

var _ auth.SessionRepository = (*SessionRepository)(nil)

func (r *SessionRepository) CreateSession(ctx context.Context, s auth.Session, hash []byte) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO user_sessions (id,user_id,secret_hash,created_at,expires_at,user_agent_summary,ip_prefix) VALUES ($1,$2,$3,$4,$5,$6,$7)`, parsePGUUID(s.ID), parsePGUUID(s.UserID), hash, s.CreatedAt, s.ExpiresAt, sessionText(s.UserAgentSummary), sessionText(s.IPPrefix))
	return err
}
func (r *SessionRepository) GetSession(ctx context.Context, id string) (auth.Session, []byte, error) {
	row := r.pool.QueryRow(ctx, `SELECT id,user_id,secret_hash,created_at,last_used_at,expires_at,revoked_at,revoked_reason,user_agent_summary,ip_prefix FROM user_sessions WHERE id=$1`, parsePGUUID(id))
	s, hash, err := scanSession(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.Session{}, nil, auth.ErrSessionNotFound
	}
	return s, hash, err
}
func (r *SessionRepository) TouchSession(ctx context.Context, id string, at time.Time) error {
	_, err := r.pool.Exec(ctx, `UPDATE user_sessions SET last_used_at=$2 WHERE id=$1 AND revoked_at IS NULL`, parsePGUUID(id), at)
	return err
}
func (r *SessionRepository) RevokeSession(ctx context.Context, id, userID, reason string, at time.Time) error {
	result, err := r.pool.Exec(ctx, `UPDATE user_sessions SET revoked_at=$3,revoked_reason=$4 WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL`, parsePGUUID(id), parsePGUUID(userID), at, sessionText(reason))
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return auth.ErrSessionNotFound
	}
	return nil
}
func (r *SessionRepository) RevokeAllUserSessions(ctx context.Context, userID, exceptID, reason string, at time.Time) error {
	_, err := r.pool.Exec(ctx, `UPDATE user_sessions SET revoked_at=$3,revoked_reason=$4 WHERE user_id=$1 AND revoked_at IS NULL AND id <> $2`, parsePGUUID(userID), parsePGUUID(exceptID), at, sessionText(reason))
	return err
}
func (r *SessionRepository) ListUserSessions(ctx context.Context, userID string) ([]auth.Session, error) {
	rows, err := r.pool.Query(ctx, `SELECT id,user_id,secret_hash,created_at,last_used_at,expires_at,revoked_at,revoked_reason,user_agent_summary,ip_prefix FROM user_sessions WHERE user_id=$1 ORDER BY created_at DESC`, parsePGUUID(userID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []auth.Session{}
	for rows.Next() {
		s, _, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

type sessionScanner interface{ Scan(...any) error }

func scanSession(row sessionScanner) (auth.Session, []byte, error) {
	var id, user pgtype.UUID
	var hash []byte
	var created, last, expires, revoked pgtype.Timestamptz
	var reason, ua, ip pgtype.Text
	if err := row.Scan(&id, &user, &hash, &created, &last, &expires, &revoked, &reason, &ua, &ip); err != nil {
		return auth.Session{}, nil, err
	}
	return auth.Session{ID: UUIDToString(id.Bytes), UserID: UUIDToString(user.Bytes), CreatedAt: created.Time, LastUsedAt: timePtr(last), ExpiresAt: expires.Time, RevokedAt: timePtr(revoked), RevokedReason: strOrZero(reason), UserAgentSummary: strOrZero(ua), IPPrefix: strOrZero(ip)}, hash, nil
}

func sessionText(value string) pgtype.Text { return pgtype.Text{String: value, Valid: value != ""} }
