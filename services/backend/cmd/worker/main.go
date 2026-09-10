package main

import (
	"context"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/ten2ten2/minoduck/services/backend/internal/jobs"
	"github.com/ten2ten2/minoduck/services/backend/internal/platform"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	if e := run(); e != nil {
		slog.Error("worker stopped", "error", e)
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
	workers := river.NewWorkers()
	w := &jobs.Worker{DB: db, Config: c, Objects: platform.Objects{Config: c}}
	river.AddWorker(workers, w)
	q, e := river.NewClient(riverpgxv5.New(db), &river.Config{
		Queues:          map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: 4}},
		Workers:         workers,
		SoftStopTimeout: 20 * time.Second,
	})
	if e != nil {
		return e
	}
	w.Queue = q
	if e = q.Start(ctx); e != nil {
		return e
	}
	<-ctx.Done()
	// River first drains active jobs, then cancels their contexts at the soft
	// deadline. Bound the final wait as well, so a stuck job cannot block deploys.
	shutdown, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	select {
	case <-q.Stopped():
		return nil
	case <-shutdown.Done():
		return shutdown.Err()
	}
}
