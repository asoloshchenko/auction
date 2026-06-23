package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/asoloshchenko/auction/internal/auth"
	"github.com/asoloshchenko/auction/internal/config"
	"github.com/asoloshchenko/auction/internal/httpapi"
	"github.com/asoloshchenko/auction/internal/service"
	"github.com/asoloshchenko/auction/internal/storage/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

const (
	tokenTTL       = 24 * time.Hour
	workerInterval = time.Second
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))
	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	if err := postgres.Migrate(ctx, cfg.DatabaseURL); err != nil {
		return err
	}
	slog.Info("migrations applied")

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return err
	}

	store := postgres.New(pool)
	tokens := auth.NewTokenManager(cfg.JWTSecret, tokenTTL)
	authSvc := auth.NewService(store, tokens)

	hub := service.NewHub(tokens)
	auctionSvc := service.NewService(store, hub)
	hub.Attach(auctionSvc)

	go service.NewWorker(auctionSvc, workerInterval).Run(ctx)

	apiHandler, err := httpapi.NewServer(authSvc, auctionSvc, tokens)
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	mux.Handle("/ws", hub)
	mux.Handle("/", apiHandler)
	srv := &http.Server{Addr: cfg.Listen, Handler: httpapi.CORS(mux)}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	slog.Info("listening", "addr", cfg.Listen)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	slog.Info("server stopped")
	return nil
}
