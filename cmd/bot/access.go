package main

import (
	"database/sql"
	"errors"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
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

// callbackRequestRegistration is the inline-button payload a stranger taps to
// start registration (ADR-0003 §4.4). The real handler lands in T-22; the T-18
// callback stub already acknowledges the tap so the client clock stops.
const callbackRequestRegistration = "req:new"

// guardOutcome is the access decision for one message sender.
type guardOutcome int

const (
	guardAllow    guardOutcome = iota // admin or approved: run the command as before
	guardPending                      // request awaiting the admin's decision
	guardDeclined                     // blacklisted
	guardUnknown                      // never registered: prompt to register
)

// outcomeFromStatus maps the admin short-circuit plus a stored status to a
// guard outcome. Pure, so the whole decision table is testable without a
// database or network.
func outcomeFromStatus(adminID, fromID int64, status AccessStatus) guardOutcome {
	if fromID == adminID {
		// The admin is not a row in users and cannot be blacklisted (ADR M3).
		return guardAllow
	}
	switch status {
	case StatusApproved:
		return guardAllow
	case StatusPending:
		return guardPending
	case StatusDeclined:
		return guardDeclined
	default: // StatusNone
		return guardUnknown
	}
}

// guardAccess resolves the outcome for a message sender. The admin short-circuits
// without touching the database; everyone else gets a fresh status read, so the
// decision always reflects the latest admin action (FR6: one query per message).
func (b *Bot) guardAccess(fromID int64) (guardOutcome, error) {
	if fromID == b.adminID {
		return guardAllow, nil
	}
	status, err := b.access.Status(fromID)
	if err != nil {
		return guardUnknown, err
	}
	return outcomeFromStatus(b.adminID, fromID, status), nil
}

// newPlainReply builds a reply WITHOUT the command keyboard: an unauthorized
// user must not be shown the /start button they cannot use yet. The approved
// flow keeps using createTextReplyMessage, which still carries that keyboard.
func newPlainReply(text string, request *tgbotapi.Message) *tgbotapi.MessageConfig {
	msg := tgbotapi.NewMessage(request.Chat.ID, text)
	msg.ReplyToMessageID = request.MessageID
	return &msg
}

// accessReply renders the status message for a user the guard did not allow
// (FR1). Only the never-registered case carries the Register button; pending
// and declined are text-only so a blacklisted user is offered nothing
// actionable.
func accessReply(outcome guardOutcome, request *tgbotapi.Message) *tgbotapi.MessageConfig {
	switch outcome {
	case guardPending:
		return newPlainReply("Your registration request is pending approval.", request)
	case guardDeclined:
		return newPlainReply("Access denied.", request)
	default: // guardUnknown
		msg := newPlainReply("Access is closed. Press the button to request registration.", request)
		msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("Register", callbackRequestRegistration),
			),
		)
		return msg
	}
}
