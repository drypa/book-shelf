package main

import (
	"context"
	"fmt"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/pkg/errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type Bot struct {
	searcher   *Searcher
	repository *Repository
	storage    *storage
	// access holds the users table for the access guard (T-21) and the callback
	// flows (T-22). adminID is the single operator's Telegram ID
	// (ADMIN_TELEGRAM_ID); the guard short-circuits on it.
	access  *AccessRepository
	adminID int64
}

func newBot(ctx context.Context, token string, adminID int64, r *Repository, s *storage, access *AccessRepository, proxy string) (*Bot, error) {

	bot, err := newTgBot(token, proxy)
	if err != nil {
		return nil, errors.Wrap(err, "create bot api")
	}
	slog.Info("bot created", "Bot", bot.Self.UserName)

	updateCfg := tgbotapi.NewUpdate(0)
	updatesChan, err := bot.GetUpdatesChan(updateCfg)
	if err != nil {
		return nil, errors.Wrap(err, "get update channel")
	}

	b := &Bot{searcher: NewSearcher(), repository: r, storage: s, access: access, adminID: adminID}
	go b.processNotifications(ctx, bot, updatesChan)

	return b, nil
}

func newTgBot(token string, proxy string) (*tgbotapi.BotAPI, error) {
	if proxy != "" {
		proxyUrl, err := url.Parse(proxy)
		if err != nil {
			slog.Error("HTTP Proxy url invalid")
			return nil, err
		}

		httpClient := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyUrl)}}

		return tgbotapi.NewBotAPIWithClient(token, httpClient)
	}
	return tgbotapi.NewBotAPI(token)
}

// updateKind is the routing target chosen for a single Telegram update.
type updateKind int

const (
	// updateKindIgnore: neither a message nor a callback (edited_message,
	// channel_post, empty update, ...): logged at debug and dropped.
	updateKindIgnore updateKind = iota
	updateKindMessage
	updateKindCallback
)

// classifyUpdate picks the branch for an update. CallbackQuery is checked
// before Message so a button press can never fall through into the command
// handler (ADR-0003 §4.3 R1–R3). Pure function: no I/O, table-testable.
func classifyUpdate(u tgbotapi.Update) updateKind {
	switch {
	case u.CallbackQuery != nil:
		return updateKindCallback
	case u.Message != nil:
		return updateKindMessage
	default:
		return updateKindIgnore
	}
}

// messageDisposition is the pre-dispatch decision for a message update.
type messageDisposition int

const (
	// messageSkipNilFrom: no sender (channel artefact) — warn and ignore (M1).
	messageSkipNilFrom messageDisposition = iota
	// messageSkipNotPrivate: group/supergroup/channel — debug and ignore (M2).
	messageSkipNotPrivate
	// messageDispatch: private message from a known sender — go to actions.
	messageDispatch
)

// decideMessage applies the guards that run before the actions map
// (ADR-0003 M1–M2). Pure function: no I/O, table-testable. A nil Chat is
// ignored rather than dereferenced.
func decideMessage(m *tgbotapi.Message) messageDisposition {
	if m == nil || m.From == nil {
		return messageSkipNilFrom
	}
	if m.Chat == nil || !m.Chat.IsPrivate() {
		return messageSkipNotPrivate
	}
	return messageDispatch
}

func (b *Bot) processNotifications(ctx context.Context, tg *tgbotapi.BotAPI, updatesChan tgbotapi.UpdatesChannel) {
	for {
		select {
		case update := <-updatesChan:
			b.handleUpdate(tg, update)
		case <-ctx.Done():
			return
		}
	}
}

// handleUpdate routes one update under a recover guard, so a panic in any
// handler is logged and the loop keeps serving the next update (ADR-0003 R0,
// fixes audit finding H1: previously an edited_message aborted the process).
func (b *Bot) handleUpdate(tg *tgbotapi.BotAPI, update tgbotapi.Update) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("panic while handling update", "update_id", update.UpdateID, "err", r)
		}
	}()

	switch classifyUpdate(update) {
	case updateKindCallback:
		b.handleCallbackStub(tg, update)
	case updateKindMessage:
		b.handleMessage(tg, update)
	default:
		slog.Debug("ignoring unsupported update", "update_id", update.UpdateID)
	}
}

// handleMessage runs the M1–M2 guards and then the existing actions dispatch
// unchanged. Command handlers and the actions map are untouched; only the dead
// "echo" tgbotapi.NewMessage construction was dropped (its Chat.ID was never
// used downstream).
func (b *Bot) handleMessage(tg *tgbotapi.BotAPI, update tgbotapi.Update) {
	msg := update.Message
	switch decideMessage(msg) {
	case messageSkipNilFrom:
		slog.Warn("ignoring message without a sender", "update_id", update.UpdateID)
		return
	case messageSkipNotPrivate:
		slog.Debug("ignoring non-private message", "update_id", update.UpdateID)
		return
	}

	a := actions[getCommand(msg.Text)]
	if a == nil {
		sendResponse(createTextReplyMessage("Command not supported", msg), tg)
		return
	}
	resp, err := a(msg.Text, b, update)
	if err != nil {
		sendResponse(createTextReplyMessage(fmt.Sprintf("Error: %v", err), msg), tg)
		return
	}
	sendResponse(resp, tg)
}

// handleCallbackStub acknowledges a button press so the Telegram client stops
// its loading clock, and takes no other action until T-22 wires the
// registration and admin-decision flows (ADR-0003 §4.4).
func (b *Bot) handleCallbackStub(tg *tgbotapi.BotAPI, update tgbotapi.Update) {
	cq := update.CallbackQuery
	slog.Debug("callback update received (unhandled until T-22)", "update_id", update.UpdateID, "data", cq.Data)
	if _, err := tg.AnswerCallbackQuery(tgbotapi.NewCallback(cq.ID, "")); err != nil {
		slog.Warn("acknowledge callback failed", "update_id", update.UpdateID, "err", err)
	}
}
func getCommand(text string) string {
	index := strings.Index(text, " ")
	if index == -1 {
		return text
	} else {
		return text[:index]
	}
}

var actions = map[string]func(string, *Bot, tgbotapi.Update) (tgbotapi.Chattable, error){
	"/start":  startAction,
	"/author": setAuthorAction,
	"/title":  setTitleAction,
	"/result": getResultsAction,
	"/get":    getFileAction,
}

func getFileAction(text string, b *Bot, update tgbotapi.Update) (tgbotapi.Chattable, error) {
	search, err := b.searcher.GetSearch(update.Message.From.ID)
	if err != nil {
		return nil, errors.Wrap(err, "get search")
	}

	param := getCommandParameter(text, "/get")
	count := len(search.Results)
	if count == 0 {
		return nil, errors.New("no results, need start new search")
	}
	bookNum := -1
	if count == 1 {
		bookNum = 1
	} else {
		if param == "" {
			return nil, errors.New(fmt.Sprintf("need set book number from 1 to %d", count))
		}
	}
	bookNum, err = strconv.Atoi(param)
	if err != nil {
		return nil, errors.New(fmt.Sprintf("need set book number from 1 to %d", count))
	}
	book := search.GetBook(bookNum)
	if book == nil {
		return nil, errors.New("book not found")
	}

	file, err := b.storage.GetFile(book.Archive, book.FileName)
	if err != nil {
		return nil, errors.Wrap(err, "get file")
	}
	share := tgbotapi.NewDocumentUpload(update.Message.Chat.ID, tgbotapi.FileBytes{
		Name:  book.GetDownloadFileName(),
		Bytes: file,
	})
	slog.Info("book downloaded", "name", book.Title, "authors", book.Authors)
	return share, nil
}

func startAction(text string, b *Bot, update tgbotapi.Update) (tgbotapi.Chattable, error) {
	_ = b.searcher.AddSearch(update.Message.From.ID)
	msg := createTextReplyMessage("new search started", update.Message)
	return msg, nil
}

func setAuthorAction(text string, b *Bot, update tgbotapi.Update) (tgbotapi.Chattable, error) {
	param := getCommandParameter(text, "/author")
	if param == "" {
		return nil, errors.New("author parameter is required")
	}
	search, err := b.searcher.GetSearch(update.Message.From.ID)
	if err != nil {
		return nil, errors.Wrap(err, "get search")
	}
	search.UpdateAuthor(param)
	books, err := b.repository.Search(search.Title, search.Author, 10, search.offset)
	if err != nil {
		return nil, errors.Wrap(err, "search")
	}
	search.SetBooks(books)
	responseText := search.GetResultsAsText()
	msg := createTextReplyMessage(responseText, update.Message)

	return msg, nil
}

func setTitleAction(text string, b *Bot, update tgbotapi.Update) (tgbotapi.Chattable, error) {
	param := getCommandParameter(text, "/title")
	if param == "" {
		return nil, errors.New("title parameter is required")
	}

	search, err := b.searcher.GetSearch(update.Message.From.ID)
	if err != nil {
		return nil, errors.Wrap(err, "get search")
	}
	search.UpdateTitle(param)

	books, err := b.repository.Search(search.Title, search.Author, 10, 0)
	if err != nil {
		return nil, errors.Wrap(err, "search")
	}
	search.SetBooks(books)
	responseText := search.GetResultsAsText()
	msg := createTextReplyMessage(responseText, update.Message)

	return msg, nil
}
func getResultsAction(text string, b *Bot, update tgbotapi.Update) (tgbotapi.Chattable, error) {
	search, err := b.searcher.GetSearch(update.Message.From.ID)
	if err != nil {
		return nil, errors.Wrap(err, "get search")
	}
	books, err := b.repository.Search(search.Title, search.Author, 10, 0)
	if err != nil {
		return nil, errors.Wrap(err, "search")
	}
	search.SetBooks(books)
	responseText := search.GetResultsAsText()
	msg := createTextReplyMessage(responseText, update.Message)
	return msg, nil
}

func getCommandParameter(messageText string, command string) string {
	return strings.TrimSpace(strings.TrimPrefix(messageText, command))
}

var replyKeyboard = tgbotapi.NewReplyKeyboard(
	tgbotapi.NewKeyboardButtonRow(
		tgbotapi.NewKeyboardButton("/start"),
	),
)

func createTextReplyMessage(message string, request *tgbotapi.Message) *tgbotapi.MessageConfig {
	msg := tgbotapi.NewMessage(request.Chat.ID, message)
	msg.ReplyToMessageID = request.MessageID
	msg.ReplyMarkup = replyKeyboard
	return &msg
}

func sendResponse(resp tgbotapi.Chattable, bot *tgbotapi.BotAPI) {
	_, err := bot.Send(resp)

	if err != nil {
		slog.Error("send response failed", "err", err)
	}
}
