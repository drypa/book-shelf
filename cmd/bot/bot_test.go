package main

import (
	"context"
	"strings"
	"testing"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
)

func privateMessage(text string) *tgbotapi.Message {
	return &tgbotapi.Message{
		MessageID: 1,
		From:      &tgbotapi.User{ID: 42},
		Chat:      &tgbotapi.Chat{ID: 42, Type: "private"},
		Text:      text,
	}
}

func TestClassifyUpdate(t *testing.T) {
	tests := []struct {
		name   string
		update tgbotapi.Update
		want   updateKind
	}{
		{
			name:   "message only",
			update: tgbotapi.Update{Message: privateMessage("/start")},
			want:   updateKindMessage,
		},
		{
			name:   "callback only",
			update: tgbotapi.Update{CallbackQuery: &tgbotapi.CallbackQuery{ID: "c1", Data: "req:new"}},
			want:   updateKindCallback,
		},
		{
			name: "callback wins over message when both present",
			update: tgbotapi.Update{
				Message:       privateMessage("/start"),
				CallbackQuery: &tgbotapi.CallbackQuery{ID: "c1"},
			},
			want: updateKindCallback,
		},
		{
			name:   "edited_message is ignored",
			update: tgbotapi.Update{EditedMessage: privateMessage("edited")},
			want:   updateKindIgnore,
		},
		{
			name:   "channel_post is ignored",
			update: tgbotapi.Update{ChannelPost: privateMessage("post")},
			want:   updateKindIgnore,
		},
		{
			name:   "empty update is ignored",
			update: tgbotapi.Update{UpdateID: 7},
			want:   updateKindIgnore,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyUpdate(tc.update); got != tc.want {
				t.Errorf("classifyUpdate() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDecideMessage(t *testing.T) {
	nilChat := &tgbotapi.Message{From: &tgbotapi.User{ID: 1}, Chat: nil, Text: "/start"}
	group := &tgbotapi.Message{
		From: &tgbotapi.User{ID: 1},
		Chat: &tgbotapi.Chat{ID: -100, Type: "group"},
		Text: "/start",
	}
	nilFrom := &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: 1, Type: "private"}, Text: "/start"}

	tests := []struct {
		name string
		msg  *tgbotapi.Message
		want messageDisposition
	}{
		{name: "nil message", msg: nil, want: messageSkipNilFrom},
		{name: "nil sender", msg: nilFrom, want: messageSkipNilFrom},
		{name: "nil chat", msg: nilChat, want: messageSkipNotPrivate},
		{name: "group chat", msg: group, want: messageSkipNotPrivate},
		{name: "private with sender", msg: privateMessage("/start"), want: messageDispatch},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := decideMessage(tc.msg); got != tc.want {
				t.Errorf("decideMessage() = %v, want %v", got, tc.want)
			}
		})
	}
}

// A nil *tgbotapi.BotAPI panics inside Send (bot.Token is dereferenced before
// any network call), which lets us exercise the recover guard hermetically.
func TestHandleUpdateContainsPanic(t *testing.T) {
	buf := captureLogs(t)
	b := &Bot{searcher: NewSearcher()}

	// Must not let the panic escape handleUpdate.
	b.handleUpdate(nil, tgbotapi.Update{UpdateID: 99, Message: privateMessage("/start")})

	logs := buf.String()
	if !strings.Contains(logs, "panic while handling update") {
		t.Fatalf("panic was not logged; logs: %s", logs)
	}
	if !strings.Contains(logs, "\"update_id\":99") {
		t.Fatalf("panic log did not include the offending update_id; logs: %s", logs)
	}
}

// The loop must survive a poisoned update and keep consuming: the second
// update proves it, and a clean shutdown after cancel proves it did not die.
func TestProcessNotificationsKeepsRunningAfterPanic(t *testing.T) {
	buf := captureLogs(t)
	b := &Bot{searcher: NewSearcher()}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// UpdatesChannel is receive-only; a bidirectional channel is assignable.
	updates := make(chan tgbotapi.Update, 2)
	// update 1: private /start handled with a nil BotAPI -> recovered panic.
	updates <- tgbotapi.Update{UpdateID: 1, Message: privateMessage("/start")}
	// update 2: unsupported update -> reached only if the loop survived update 1.
	updates <- tgbotapi.Update{UpdateID: 2, EditedMessage: privateMessage("edited")}

	done := make(chan struct{})
	go func() {
		b.processNotifications(ctx, nil, updates)
		close(done)
	}()

	// Let the loop drain both buffered updates, then shut it down.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("processNotifications did not return after cancel; the loop likely died on the panic")
	}

	if got := buf.String(); !strings.Contains(got, "panic while handling update") {
		t.Fatalf("expected the panic to be logged; logs: %s", got)
	}
}
