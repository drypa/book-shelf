package main

import (
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
)

func TestOutcomeFromStatus(t *testing.T) {
	const admin int64 = 1
	const user int64 = 42

	tests := []struct {
		name   string
		fromID int64
		status AccessStatus
		want   guardOutcome
	}{
		{name: "admin with no row", fromID: admin, status: StatusNone, want: guardAllow},
		{name: "admin with a declined row", fromID: admin, status: StatusDeclined, want: guardAllow},
		{name: "admin with a pending row", fromID: admin, status: StatusPending, want: guardAllow},
		{name: "user approved", fromID: user, status: StatusApproved, want: guardAllow},
		{name: "user pending", fromID: user, status: StatusPending, want: guardPending},
		{name: "user declined", fromID: user, status: StatusDeclined, want: guardDeclined},
		{name: "user none", fromID: user, status: StatusNone, want: guardUnknown},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := outcomeFromStatus(admin, tc.fromID, tc.status); got != tc.want {
				t.Errorf("outcomeFromStatus() = %v, want %v", got, tc.want)
			}
		})
	}
}

// FR6: the decision is re-read from the database on every message, so a change
// to the stored row between two messages changes the outcome.
func TestGuardAccessReflectsDatabaseChanges(t *testing.T) {
	repo := newTestAccessRepo(t)
	b := &Bot{access: repo, adminID: 999}

	if got, err := b.guardAccess(42); err != nil || got != guardUnknown {
		t.Fatalf("unknown user: got (%v, %v), want (guardUnknown, nil)", got, err)
	}
	if _, err := repo.CreatePending(sampleProfile(42)); err != nil {
		t.Fatalf("CreatePending: %v", err)
	}
	if got, err := b.guardAccess(42); err != nil || got != guardPending {
		t.Fatalf("after request: got (%v, %v), want (guardPending, nil)", got, err)
	}
	if _, _, err := repo.Decide(42, StatusApproved); err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if got, err := b.guardAccess(42); err != nil || got != guardAllow {
		t.Fatalf("after approval: got (%v, %v), want (guardAllow, nil)", got, err)
	}
}

// The admin is allowed even if the users table somehow holds a declined row for
// their ID: the short-circuit runs before the database read.
func TestGuardAccessAdminShortCircuitsDatabase(t *testing.T) {
	repo := newTestAccessRepo(t)
	if _, err := repo.CreatePending(sampleProfile(999)); err != nil {
		t.Fatalf("CreatePending: %v", err)
	}
	if _, _, err := repo.Decide(999, StatusDeclined); err != nil {
		t.Fatalf("Decide: %v", err)
	}
	b := &Bot{access: repo, adminID: 999}

	if got, err := b.guardAccess(999); err != nil || got != guardAllow {
		t.Fatalf("admin with a declined row: got (%v, %v), want (guardAllow, nil)", got, err)
	}
}

// Status replies must not carry the command keyboard; the approved flow keeps
// it. Only the unknown prompt adds an inline Register button.
func TestAccessReplyKeyboardContract(t *testing.T) {
	request := privateMessage("/start")

	if cfg := newPlainReply("x", request); cfg.ReplyMarkup != nil {
		t.Fatalf("newPlainReply must have no markup, got %#v", cfg.ReplyMarkup)
	}
	for _, outcome := range []guardOutcome{guardPending, guardDeclined} {
		if cfg := accessReply(outcome, request); cfg.ReplyMarkup != nil {
			t.Fatalf("status reply %v must have no markup, got %#v", outcome, cfg.ReplyMarkup)
		}
	}

	unknown := accessReply(guardUnknown, request)
	if _, ok := unknown.ReplyMarkup.(tgbotapi.InlineKeyboardMarkup); !ok {
		t.Fatalf("unknown reply must carry an inline keyboard, got %#v", unknown.ReplyMarkup)
	}

	approved := createTextReplyMessage("ok", request)
	if _, ok := approved.ReplyMarkup.(tgbotapi.ReplyKeyboardMarkup); !ok {
		t.Fatalf("approved reply must keep the command keyboard, got %#v", approved.ReplyMarkup)
	}
}
