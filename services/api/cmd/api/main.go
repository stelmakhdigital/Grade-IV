// Command api — HTTP-сервис «Грейд»: сессии, аутентификация, оркестрация (ARCHITECTURE.md).
// WP-1: каркас + health; WP-2: модель данных + auth. Дальнейшие WP — см. roadmap.md.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/config"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/db"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/httpapi"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config", "err", err)
		os.Exit(1)
	}
	// LOG_LEVEL (info|debug) — применяется к логгеру (баг Фазы 4: читалось
	// из окружения, но не использовалось).
	var level slog.Level
	switch cfg.LogLevel {
	case "debug":
		level = slog.LevelDebug
	default:
		level = slog.LevelInfo
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	if cfg.JWTSecret == config.DevInsecureSecret {
		logger.Warn("JWT_SECRET не задан — используется dev-секрет (только для разработки)")
	}

	conn, dialect, err := db.Open(cfg.DatabaseURL)
	if err != nil {
		logger.Error("open database", "err", err)
		os.Exit(1)
	}
	defer conn.Close()

	ctx := context.Background()
	if err := db.Migrate(ctx, conn, dialect); err != nil {
		logger.Error("migrate", "err", err)
		os.Exit(1)
	}

	engineCtx, engineCancel := context.WithCancel(ctx)
	defer engineCancel()
	server := httpapi.New(cfg, conn, dialect, logger)
	go server.Engine().Run(engineCtx)
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		logger.Info("api started", "addr", cfg.Addr, "dialect", string(dialect))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("listen", "err", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown", "err", err)
	}
	// После остановки HTTP останавливаем таймер сессий (Detach/рекавери уже не нужны).
	server.Engine().Stop()
	logger.Info("api stopped")
}
