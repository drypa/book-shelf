package main

import (
	"log/slog"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
)

// Callback data (Telegram limits it to 1–64 bytes; the format is frozen because
// it is baked into message history and must survive bot restarts) — ADR-0003 §4.4.
const (
	cbRegister      = "req:new"
	cbApprovePrefix = "adm:approve:"
	cbDeclinePrefix = "adm:decline:"
)

// parseAdminAction decodes "adm:approve:<id>" / "adm:decline:<id>" into the
// target Telegram ID and the decision. Anything else (unknown prefix, empty or
// non-numeric tail, non-positive id) returns ok=false.
func parseAdminAction(data string) (targetID int64, approve bool, ok bool) {
	var rest string
	switch {
	case strings.HasPrefix(data, cbApprovePrefix):
		rest, approve = data[len(cbApprovePrefix):], true
	case strings.HasPrefix(data, cbDeclinePrefix):
		rest, approve = data[len(cbDeclinePrefix):], false
	default:
		return 0, false, false
	}
	id, err := strconv.ParseInt(rest, 10, 64)
	if err != nil || id <= 0 {
		return 0, false, false
	}
	return id, approve, true
}

// formatAdminRequest renders the registration card sent to the admin (FR3).
// Only fields Telegram actually provided are shown.
func formatAdminRequest(p UserProfile) string {
	var b strings.Builder
	b.WriteString("New registration request\n")
	b.WriteString("ID: ")
	b.WriteString(strconv.FormatInt(p.TelegramID, 10))
	b.WriteString("\nName: ")
	b.WriteString(p.FirstName)
	if p.LastName != "" {
		b.WriteString("\nLast name: ")
		b.WriteString(p.LastName)
	}
	if p.UserName != "" {
		b.WriteString("\nUsername: @")
		b.WriteString(p.UserName)
	}
	if p.LanguageCode != "" {
		b.WriteString("\nLanguage: ")
		b.WriteString(p.LanguageCode)
	}
	return b.String()
}

// decisionAlert is the admin-facing message when a decision cannot be applied
// because the stored state already is terminal (ADR-0003 C3). Pure, so the
// mapping is testable without a database.
func decisionAlert(current AccessStatus, approve bool) string {
	switch current {
	case StatusApproved:
		if approve {
			return "User is already approved."
		}
		return "Access revocation is out of scope."
	case StatusDeclined:
		return "User has already been declined."
	default: // StatusNone — no request exists for this user
		return "No pending request for this user."
	}
}

// profileFromUser snapshots a Telegram user for the users table (FR3).
func profileFromUser(id int64, u *tgbotapi.User) UserProfile {
	if u == nil {
		return UserProfile{TelegramID: id}
	}
	return UserProfile{
		TelegramID:   id,
		FirstName:    u.FirstName,
		LastName:     u.LastName,
		UserName:     u.UserName,
		LanguageCode: u.LanguageCode,
	}
}

// handleCallback routes callback_query updates to the registration or
// admin-decision handler, and rejects anything else (ADR-0003 C10).
func (b *Bot) handleCallback(tg *tgbotapi.BotAPI, update tgbotapi.Update) {
	cq := update.CallbackQuery
	if cq.From == nil {
		slog.Warn("ignoring callback without a sender", "update_id", update.UpdateID)
		return
	}
	switch {
	case cq.Data == cbRegister:
		b.handleRegistrationRequest(tg, cq)
	case strings.HasPrefix(cq.Data, cbApprovePrefix), strings.HasPrefix(cq.Data, cbDeclinePrefix):
		b.handleAdminDecision(tg, cq)
	default:
		slog.Warn("unsupported callback data", "update_id", update.UpdateID, "data", cq.Data)
		answerCallback(tg, cq.ID, "Action not supported", true)
	}
}

// registrationTapAlert handles a req:new tap that cannot lead to a new request:
// a declined user must not be able to request again (the old button may still
// exist in their chat history), and an approved one has nothing to request.
// handled=false means the tap may proceed (none or pending — pending re-sends,
// C6). Pure, table-testable.
func registrationTapAlert(status AccessStatus) (alert string, handled bool) {
	switch status {
	case StatusDeclined:
		return "Your registration request was declined.", true
	case StatusApproved:
		return "Your access is already approved.", true
	default: // StatusNone, StatusPending
		return "", false
	}
}

// handleRegistrationRequest implements req:new (ADR-0003 C5–C8).
func (b *Bot) handleRegistrationRequest(tg *tgbotapi.BotAPI, cq *tgbotapi.CallbackQuery) {
	fromID := int64(cq.From.ID)
	if fromID == b.adminID {
		answerCallback(tg, cq.ID, "Admin does not need registration.", true) // C8
		return
	}

	status, err := b.access.Status(fromID)
	if err != nil {
		slog.Error("read access status failed", "telegram_id", fromID, "err", err)
		answerCallback(tg, cq.ID, "Something went wrong, please try again later.", true)
		return
	}
	if alert, handled := registrationTapAlert(status); handled {
		answerCallback(tg, cq.ID, alert, true)
		return
	}

	profile := profileFromUser(fromID, cq.From)
	created, err := b.access.CreatePending(profile)
	if err != nil {
		slog.Error("create pending request failed", "telegram_id", fromID, "err", err)
		answerCallback(tg, cq.ID, "Something went wrong, please try again later.", true)
		return
	}

	// Deliver the request to the admin. This runs for a fresh request AND for a
	// repeat tap (C6): if the first delivery was lost (RR1: the admin must have
	// started the bot at least once), the second tap re-sends it. Extra copies
	// are harmless — the decision is keyed by telegram_id, so the first acted-on
	// button wins and the rest hit the terminal-state alerts (C3).
	if sendErr := b.sendAdminRequest(tg, profile); sendErr != nil {
		slog.Error("deliver registration request to admin failed", "telegram_id", fromID, "err", sendErr)
		answerCallback(tg, cq.ID, "Failed to notify the admin, please try again.", true) // C7
		return
	}

	if created {
		answerCallback(tg, cq.ID, "Registration request sent, wait for the admin.", false)
	} else {
		answerCallback(tg, cq.ID, "Request already sent, wait for the admin.", false)
	}
}

// handleAdminDecision implements adm:approve:<id> / adm:decline:<id>
// (ADR-0003 C1–C5, C9).
func (b *Bot) handleAdminDecision(tg *tgbotapi.BotAPI, cq *tgbotapi.CallbackQuery) {
	// C1: only the admin may decide; a button forwarded to someone else grants
	// nothing — the callback identity comes from the Telegram transport.
	if int64(cq.From.ID) != b.adminID {
		answerCallback(tg, cq.ID, "You are not allowed to decide.", true)
		return
	}

	targetID, approve, ok := parseAdminAction(cq.Data)
	if !ok { // defensive; the dispatcher already filtered on the prefixes
		answerCallback(tg, cq.ID, "Action not supported", true)
		return
	}
	// C9: a hand-forged callback must never turn the admin into a managed user.
	if targetID == b.adminID {
		answerCallback(tg, cq.ID, "The admin cannot be blacklisted.", true)
		return
	}

	to := StatusDeclined
	if approve {
		to = StatusApproved
	}
	applied, current, err := b.access.Decide(targetID, to)
	if err != nil {
		slog.Error("decide registration failed", "telegram_id", targetID, "err", err)
		answerCallback(tg, cq.ID, "Something went wrong, please try again later.", true)
		return
	}
	if !applied { // C3: double tap / stale button / terminal state — nothing changed
		answerCallback(tg, cq.ID, decisionAlert(current, approve), true)
		return
	}

	// C4: extinguish the request message (remove its keyboard). A failure here
	// is not fatal — the decision is already persisted; a second tap hits C3.
	b.dismissAdminRequest(tg, cq)
	// Notify the decided-upon user; a failure is only logged (the truth is the
	// DB row, and the user learns the status on their next message).
	b.notifyUser(tg, targetID, to)

	result := "User declined."
	if approve {
		result = "User approved."
	}
	answerCallback(tg, cq.ID, result, false) // C5
}

// sendAdminRequest delivers the registration card to the admin's chat with the
// Accept / Decline inline keyboard keyed by the requester's telegram_id.
func (b *Bot) sendAdminRequest(tg *tgbotapi.BotAPI, p UserProfile) error {
	msg := tgbotapi.NewMessage(b.adminID, formatAdminRequest(p))
	msg.ReplyMarkup = adminDecisionKeyboard(p.TelegramID)
	_, err := tg.Send(msg)
	return err
}

func adminDecisionKeyboard(targetID int64) tgbotapi.InlineKeyboardMarkup {
	id := strconv.FormatInt(targetID, 10)
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Accept", cbApprovePrefix+id),
			tgbotapi.NewInlineKeyboardButtonData("Decline", cbDeclinePrefix+id),
		),
	)
}

// dismissAdminRequest replaces the handled request message with a short summary
// and removes its keyboard (empty, non-nil inline markup -> {"inline_keyboard":[]}).
func (b *Bot) dismissAdminRequest(tg *tgbotapi.BotAPI, cq *tgbotapi.CallbackQuery) {
	if cq.Message == nil {
		return
	}
	edit := tgbotapi.NewEditMessageText(cq.Message.Chat.ID, cq.Message.MessageID, "Registration request handled.")
	// An explicit empty (non-nil) keyboard marshals to {"inline_keyboard":[]},
	// which is what Telegram requires to remove the buttons. The helper
	// NewInlineKeyboardMarkup() with no rows would serialize to null instead.
	empty := tgbotapi.InlineKeyboardMarkup{InlineKeyboard: make([][]tgbotapi.InlineKeyboardButton, 0)}
	edit.ReplyMarkup = &empty
	if _, err := tg.Send(edit); err != nil {
		slog.Warn("dismiss admin request failed", "err", err)
	}
}

// notifyUser tells the decided-upon user the result (FR8).
func (b *Bot) notifyUser(tg *tgbotapi.BotAPI, userID int64, status AccessStatus) {
	text := "Your registration request was declined."
	if status == StatusApproved {
		text = "Your access has been approved. You can use the library now."
	}
	if _, err := tg.Send(tgbotapi.NewMessage(userID, text)); err != nil {
		slog.Warn("notify user failed", "telegram_id", userID, "err", err)
	}
}

// answerCallback ends every handled callback (C5): showAlert=false is a quiet
// toast, true is a blocking dialog for the admin's contested actions.
func answerCallback(tg *tgbotapi.BotAPI, callbackID, text string, showAlert bool) {
	cfg := tgbotapi.NewCallback(callbackID, text)
	cfg.ShowAlert = showAlert
	if _, err := tg.AnswerCallbackQuery(cfg); err != nil {
		slog.Warn("answer callback failed", "err", err)
	}
}
