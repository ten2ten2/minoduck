package main

import (
	"context"
	"errors"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/ten2ten2/minoduck/services/backend/internal/httpapi"
	"github.com/ten2ten2/minoduck/services/backend/internal/platform"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	if e := run(); e != nil {
		slog.Error("API stopped", "error", e)
		os.Exit(1)
	}
}
func run() error {
	c, e := platform.Load()
	if e != nil {
		return e
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	db, e := platform.Open(ctx, c)
	if e != nil {
		return e
	}
	defer db.Close()
	q, e := river.NewClient(riverpgxv5.New(db), &river.Config{})
	if e != nil {
		return e
	}
	app := &httpapi.Server{Config: c, DB: db, Queue: q, Objects: platform.Objects{Config: c}}
	server := &http.Server{Addr: ":" + c.Port, Handler: app.Router(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 60 * time.Second, WriteTimeout: 90 * time.Second, IdleTimeout: 60 * time.Second}
	errs := make(chan error, 1)
	go func() { errs <- server.ListenAndServe() }()
	slog.Info("MinoDuck API ready", "port", c.Port)
	select {
	case e := <-errs:
		if errors.Is(e, http.ErrServerClosed) {
			return nil
		}
		return e
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return server.Shutdown(shutdown)
	}
}
