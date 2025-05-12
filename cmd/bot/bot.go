package main

import (
	"context"
	"fmt"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/pkg/errors"
	"log"
	"log/slog"
	"strconv"
	"strings"
)

type Bot struct {
	searcher   *Searcher
	repository *Repository
	storage    *storage
}

func newBot(ctx context.Context, token string, r *Repository, s *storage) (*Bot, error) {

	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, errors.Wrap(err, "create bot api")
	}
	slog.Info("bot created", "Bot", bot.Self.UserName)

	updateCfg := tgbotapi.NewUpdate(0)
	updatesChan, err := bot.GetUpdatesChan(updateCfg)
	if err != nil {
		return nil, errors.Wrap(err, "get update channel")
	}

	b := &Bot{searcher: NewSearcher(), repository: r, storage: s}
	go b.processNotifications(ctx, bot, updatesChan)

	return b, nil
}

func (b *Bot) processNotifications(ctx context.Context, tg *tgbotapi.BotAPI, updatesChan tgbotapi.UpdatesChannel) {
	for {
		select {
		case update := <-updatesChan:
			msg := tgbotapi.NewMessage(update.Message.Chat.ID, update.Message.Text)

			a := actions[getCommand(msg.Text)]
			if a == nil {
				sendResponse(createTextReplyMessage("Command not supported", update.Message), tg)
				continue
			}
			resp, err := a(msg.Text, b, update, tg)
			if err != nil {
				sendResponse(createTextReplyMessage(fmt.Sprintf("Error: %v", err), update.Message), tg)
				continue
			}
			sendResponse(resp, tg)

		case <-ctx.Done():
			return
		}
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

var actions = map[string]func(string, *Bot, tgbotapi.Update, *tgbotapi.BotAPI) (tgbotapi.Chattable, error){
	"/start":  startAction,
	"/author": setAuthorAction,
	"/title":  setTitleAction,
	"/result": getResultsAction,
	"/get":    getFileAction,
}

func getFileAction(text string, b *Bot, update tgbotapi.Update, tg *tgbotapi.BotAPI) (tgbotapi.Chattable, error) {
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
		Name:  book.FileName,
		Bytes: file,
	})

	return share, nil
}

func startAction(text string, b *Bot, update tgbotapi.Update, tg *tgbotapi.BotAPI) (tgbotapi.Chattable, error) {
	_ = b.searcher.AddSearch(update.Message.From.ID)
	msg := createTextReplyMessage("new search started", update.Message)
	return msg, nil
}

func setAuthorAction(text string, b *Bot, update tgbotapi.Update, tg *tgbotapi.BotAPI) (tgbotapi.Chattable, error) {
	param := getCommandParameter(text, "/author")
	if param == "" {
		return nil, errors.New("author parameter is required")
	}
	search, err := b.searcher.GetSearch(update.Message.From.ID)
	if err != nil {
		return nil, errors.Wrap(err, "get search")
	}
	search.UpdateAuthor(param)
	books, err := b.repository.Search(search.Title, search.Author)
	if err != nil {
		return nil, errors.Wrap(err, "search")
	}
	search.SetBooks(books)
	responseText := search.GetResultsAsText()
	msg := createTextReplyMessage(responseText, update.Message)

	return msg, nil
}

func setTitleAction(text string, b *Bot, update tgbotapi.Update, tg *tgbotapi.BotAPI) (tgbotapi.Chattable, error) {
	param := getCommandParameter(text, "/title")
	if param == "" {
		return nil, errors.New("title parameter is required")
	}

	search, err := b.searcher.GetSearch(update.Message.From.ID)
	if err != nil {
		return nil, errors.Wrap(err, "get search")
	}
	search.UpdateTitle(param)

	books, err := b.repository.Search(search.Title, search.Author)
	if err != nil {
		return nil, errors.Wrap(err, "search")
	}
	search.SetBooks(books)
	responseText := search.GetResultsAsText()
	msg := createTextReplyMessage(responseText, update.Message)

	return msg, nil
}
func getResultsAction(text string, b *Bot, update tgbotapi.Update, tg *tgbotapi.BotAPI) (tgbotapi.Chattable, error) {
	search, err := b.searcher.GetSearch(update.Message.From.ID)
	if err != nil {
		return nil, errors.Wrap(err, "get search")
	}
	books, err := b.repository.Search(search.Title, search.Author)
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
		log.Println(err)
	}
}
