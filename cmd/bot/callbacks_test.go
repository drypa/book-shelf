package main

import (
	"encoding/json"
	"strings"
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
)

// Locks the C4 fix: the "remove keyboard" markup must serialize to an empty
// array ({"inline_keyboard":[]}), not null, or Telegram will not drop buttons.
func TestDismissKeyboardSerializesToEmptyArray(t *testing.T) {
	empty := tgbotapi.InlineKeyboardMarkup{InlineKeyboard: make([][]tgbotapi.InlineKeyboardButton, 0)}
	b, err := json.Marshal(empty)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(b) != `{"inline_keyboard":[]}` {
		t.Fatalf("empty keyboard serialized to %s, want {\"inline_keyboard\":[]}", b)
	}
}

func TestParseAdminAction(t *testing.T) {
	tests := []struct {
		data     string
		wantID   int64
		wantAppr bool
		wantOK   bool
	}{
		{data: "adm:approve:123", wantID: 123, wantAppr: true, wantOK: true},
		{data: "adm:decline:456", wantID: 456, wantAppr: false, wantOK: true},
		{data: "adm:approve:9007199254740993", wantID: 9007199254740993, wantAppr: true, wantOK: true},
		{data: "req:new", wantOK: false},
		{data: "adm:approve:", wantOK: false},
		{data: "adm:decline:", wantOK: false},
		{data: "adm:approve:abc", wantOK: false},
		{data: "adm:approve:12x", wantOK: false},
		{data: "adm:approve:0", wantOK: false},
		{data: "adm:approve:-5", wantOK: false},
		{data: "adm:promote:5", wantOK: false},
		{data: "", wantOK: false},
	}

	for _, tc := range tests {
		t.Run(tc.data, func(t *testing.T) {
			id, approve, ok := parseAdminAction(tc.data)
			if ok != tc.wantOK || id != tc.wantID || approve != tc.wantAppr {
				t.Errorf("parseAdminAction(%q) = (%d, %v, %v), want (%d, %v, %v)",
					tc.data, id, approve, ok, tc.wantID, tc.wantAppr, tc.wantOK)
			}
		})
	}
}

// The keyboard produced for the admin must round-trip through the parser, or a
// pressed button could target the wrong user.
func TestAdminDecisionKeyboardRoundTrip(t *testing.T) {
	kb := adminDecisionKeyboard(777)
	if len(kb.InlineKeyboard) != 1 || len(kb.InlineKeyboard[0]) != 2 {
		t.Fatalf("keyboard shape = %#v, want 1 row with 2 buttons", kb.InlineKeyboard)
	}

	accept := kb.InlineKeyboard[0][0]
	if id, approve, ok := parseAdminAction(*accept.CallbackData); !ok || !approve || id != 777 {
		t.Errorf("accept button data %q parsed as (%d, %v, %v)", *accept.CallbackData, id, approve, ok)
	}
	decline := kb.InlineKeyboard[0][1]
	if id, approve, ok := parseAdminAction(*decline.CallbackData); !ok || approve || id != 777 {
		t.Errorf("decline button data %q parsed as (%d, %v, %v)", *decline.CallbackData, id, approve, ok)
	}
}

func TestFormatAdminRequest(t *testing.T) {
	t.Run("all fields present", func(t *testing.T) {
		text := formatAdminRequest(UserProfile{
			TelegramID:   123456789,
			FirstName:    "Ivan",
			LastName:     "Petrov",
			UserName:     "ivan",
			LanguageCode: "ru",
		})
		for _, want := range []string{
			"New registration request",
			"ID: 123456789",
			"Name: Ivan",
			"Last name: Petrov",
			"Username: @ivan",
			"Language: ru",
		} {
			if !strings.Contains(text, want) {
				t.Errorf("request text missing %q:\n%s", want, text)
			}
		}
	})

	t.Run("optional fields omitted when Telegram did not provide them", func(t *testing.T) {
		text := formatAdminRequest(UserProfile{TelegramID: 42, FirstName: "Ann"})
		if strings.Contains(text, "Last name") {
			t.Errorf("text must not contain Last name line:\n%s", text)
		}
		if strings.Contains(text, "Username") {
			t.Errorf("text must not contain Username line:\n%s", text)
		}
		if strings.Contains(text, "Language") {
			t.Errorf("text must not contain Language line:\n%s", text)
		}
		if !strings.Contains(text, "ID: 42") || !strings.Contains(text, "Name: Ann") {
			t.Errorf("mandatory lines missing:\n%s", text)
		}
	})
}

func TestDecisionAlertMapping(t *testing.T) {
	tests := []struct {
		name    string
		current AccessStatus
		approve bool
		want    string
	}{
		{name: "already approved via approve", current: StatusApproved, approve: true, want: "User is already approved."},
		{name: "decline an approved user is out of scope", current: StatusApproved, approve: false, want: "Access revocation is out of scope."},
		{name: "approve after decline changes nothing", current: StatusDeclined, approve: true, want: "User has already been declined."},
		{name: "decline after decline", current: StatusDeclined, approve: false, want: "User has already been declined."},
		{name: "no request at all", current: StatusNone, approve: true, want: "No pending request for this user."},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := decisionAlert(tc.current, tc.approve); got != tc.want {
				t.Errorf("decisionAlert(%q, %v) = %q, want %q", tc.current, tc.approve, got, tc.want)
			}
		})
	}
}

// The repository-level guarantee behind the callback handlers (C3/C6): repeated
// or out-of-order decisions never change a terminal state. Uses the real repo
// on :memory: (also asserted in access_test.go; kept here because it is the
// exact sequence the admin buttons produce).
func TestCallbackDecisionSequencesOnRepository(t *testing.T) {
	t.Run("double approve", func(t *testing.T) {
		repo := newTestAccessRepo(t)
		if _, err := repo.CreatePending(sampleProfile(5)); err != nil {
			t.Fatalf("CreatePending: %v", err)
		}
		if applied, _, err := repo.Decide(5, StatusApproved); err != nil || !applied {
			t.Fatalf("first approve: applied=%v err=%v", applied, err)
		}
		applied, current, err := repo.Decide(5, StatusApproved)
		if err != nil {
			t.Fatalf("second approve: %v", err)
		}
		if applied || current != StatusApproved {
			t.Fatalf("second approve changed state: applied=%v current=%q", applied, current)
		}
	})

	t.Run("request re-tap never duplicates", func(t *testing.T) {
		repo := newTestAccessRepo(t)
		if created, err := repo.CreatePending(sampleProfile(6)); err != nil || !created {
			t.Fatalf("first CreatePending: created=%v err=%v", created, err)
		}
		if created, err := repo.CreatePending(sampleProfile(6)); err != nil || created {
			t.Fatalf("repeat CreatePending must no-op: created=%v err=%v", created, err)
		}
	})
}

// The unknown-callback branch and the guard rails must not dereference a nil
// From/Message: handleCallback drops such updates (the recover in handleUpdate
// is the last line of defense, not the design).
func TestHandleCallbackNilSenderIsIgnored(t *testing.T) {
	b := &Bot{}
	update := tgbotapi.Update{CallbackQuery: &tgbotapi.CallbackQuery{ID: "c1", Data: cbRegister}}

	// No panic, no network: tg is never touched because the update is dropped.
	b.handleCallback(nil, update)
}
