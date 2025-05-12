package main

import (
	"context"
	"database/sql"
	"fmt"
	_ "github.com/mattn/go-sqlite3"
	"github.com/spf13/viper"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	viper.AutomaticEnv()
	dbPath := viper.GetString("DB_PATH")
	if dbPath == "" {
		slog.Error("DB_PATH is not set")
		return
	}

	botToken := viper.GetString("BOT_TOKEN")
	if botToken == "" {
		slog.Error("BOT_TOKEN is not set")
		return
	}

	storage, err := newStorage(dbPath)
	if err != nil {
		slog.Error("failed to init storage", err)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	connectionString := viper.GetString("DB_CONNECTION_STRING")
	if connectionString == "" {
		slog.Error("DB_CONNECTION_STRING is not set")
		return
	}
	db, err := sql.Open("sqlite3", connectionString)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer db.Close()

	repository := NewRepository(db)
	_, err = newBot(ctx, botToken, repository, storage)
	if err != nil {
		log.Fatal(err)
	}

	<-quit
	slog.Info("Shutting down...")
	cancel()
}
