package main

import (
	"database/sql"
	"errors"
	"time"
)

// AccessStatus is the persisted access state of a Telegram user. The zero value
// StatusNone means there is no row in the users table (the user never sent a
// registration request).
type AccessStatus string

const (
	StatusNone     AccessStatus = ""
	StatusPending  AccessStatus = "pending"
	StatusApproved AccessStatus = "approved"
	StatusDeclined AccessStatus = "declined"
)

// UserProfile is the Telegram snapshot captured at registration time (FR3). It
// is stored verbatim so the admin decides on exactly the data Telegram gave at
// that moment. UserName carries no leading '@', exactly as Telegram reports it.
type UserProfile struct {
	TelegramID   int64
	FirstName    string
	LastName     string
	UserName     string
	LanguageCode string
}

// AccessRepository owns the bot-private users table: registration requests and
// the admin's approve/decline decisions. It is deliberately kept out of the
// storage package (the shared books row model) and out of scan/db-create — the
// table belongs to the bot and is created by the bot itself at startup
// (ADR-0003 §4.1, §4.5).
type AccessRepository struct {
	db *sql.DB
}

func NewAccessRepository(db *sql.DB) *AccessRepository {
	return &AccessRepository{db: db}
}

// EnsureSchema creates the users table if it is absent. The change is additive
// (books is untouched) and idempotent, so starting against a database that
// predates access control, or restarting, is always safe.
func (r *AccessRepository) EnsureSchema() error {
	const schema = `
CREATE TABLE IF NOT EXISTS users (
    telegram_id   INTEGER PRIMARY KEY,
    status        TEXT NOT NULL CHECK (status IN ('pending','approved','declined')),
    first_name    TEXT NOT NULL DEFAULT '',
    last_name     TEXT NOT NULL DEFAULT '',
    username      TEXT NOT NULL DEFAULT '',
    language_code TEXT NOT NULL DEFAULT '',
    created_at    TEXT NOT NULL,
    decided_at    TEXT
);`
	_, err := r.db.Exec(schema)
	return err
}

// Status returns the current access status of a Telegram user, or StatusNone
// when there is no row.
func (r *AccessRepository) Status(telegramID int64) (AccessStatus, error) {
	var status AccessStatus
	err := r.db.QueryRow(`SELECT status FROM users WHERE telegram_id = ?`, telegramID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return StatusNone, nil
	}
	if err != nil {
		return StatusNone, err
	}
	return status, nil
}

// CreatePending inserts a pending request with the captured profile. ON CONFLICT
// DO NOTHING makes "exactly one active request per user" (FR2) atomic at the
// schema level: a second call is a no-op that reports created=false and leaves
// the stored profile untouched.
func (r *AccessRepository) CreatePending(p UserProfile) (bool, error) {
	res, err := r.db.Exec(`
INSERT INTO users (telegram_id, status, first_name, last_name, username, language_code, created_at)
VALUES (?, 'pending', ?, ?, ?, ?, ?)
ON CONFLICT(telegram_id) DO NOTHING`,
		p.TelegramID, p.FirstName, p.LastName, p.UserName, p.LanguageCode, nowRFC3339())
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// Decide applies the admin's decision to a still-pending request. The
// WHERE status='pending' makes the transition atomic and one-way: a second
// attempt (double tap, stale button after restart) affects zero rows and
// reports applied=false, while current tells the caller the real state so it
// can answer the admin precisely. approved/declined are terminal — there is no
// transition out of them (revoke/unban are out of scope).
func (r *AccessRepository) Decide(telegramID int64, to AccessStatus) (applied bool, current AccessStatus, err error) {
	res, err := r.db.Exec(`
UPDATE users SET status = ?, decided_at = ?
WHERE telegram_id = ? AND status = 'pending'`,
		string(to), nowRFC3339(), telegramID)
	if err != nil {
		return false, StatusNone, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, StatusNone, err
	}
	if n == 0 {
		current, err := r.Status(telegramID)
		return false, current, err
	}
	return true, to, nil
}

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}
