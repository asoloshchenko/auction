package service

import (
	"context"
	"log/slog"
	"time"
)

type Worker struct {
	svc      *Service
	interval time.Duration
}

func NewWorker(svc *Service, interval time.Duration) *Worker {
	return &Worker{svc: svc, interval: interval}
}

func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.tick(ctx)
		}
	}
}

func (w *Worker) tick(ctx context.Context) {
	if err := w.svc.CloseExpiredAuctions(ctx); err != nil {
		slog.ErrorContext(ctx, "worker: close expired auctions", "error", err)
	}
	if err := w.svc.NotifyEndingSoon(ctx); err != nil {
		slog.ErrorContext(ctx, "worker: notify ending soon", "error", err)
	}
}
