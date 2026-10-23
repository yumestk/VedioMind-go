package main

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/yumestk/VedioMind-go/internal/app"
	"github.com/yumestk/VedioMind-go/migrations"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
func run() error {
	cfg, err := app.LoadConfig()
	if err != nil {
		return err
	}
	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(12)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(30 * time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	err = db.PingContext(ctx)
	if err == nil {
		err = migrations.Up(ctx, db)
	}
	cancel()
	if err != nil {
		return err
	}
	if len(os.Args) > 1 && os.Args[1] == "migrate" {
		return nil
	}
	application := app.New(db, cfg)
	server := &http.Server{Addr: cfg.Address, Handler: application.Routes(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 5 * time.Minute, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	stop, done := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer done()
	application.RunWorkers(stop)
	defer func() { done(); application.WaitWorkers() }()
	errors := make(chan error, 1)
	go func() { slog.Info("HTTP listening", "address", cfg.Address); errors <- server.ListenAndServe() }()
	select {
	case err = <-errors:
		if err != http.ErrServerClosed {
			return err
		}
	case <-stop.Done():
	}
	shutdown, close := context.WithTimeout(context.Background(), 15*time.Second)
	defer close()
	return server.Shutdown(shutdown)
}
