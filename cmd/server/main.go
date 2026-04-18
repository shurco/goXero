package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/shurco/goxero/internal/config"
	"github.com/shurco/goxero/internal/database"
	"github.com/shurco/goxero/internal/logger"
	"github.com/shurco/goxero/internal/models"
	"github.com/shurco/goxero/internal/repository"
	"github.com/shurco/goxero/internal/router"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config error:", err)
		os.Exit(1)
	}

	log := logger.New(cfg.App.LogLevel)
	slog.SetDefault(log)

	log.Info("starting goxero",
		"env", cfg.App.Environment,
		"port", cfg.Server.Port,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool, err := database.NewPool(ctx, cfg.Database)
	if err != nil {
		log.Error("database connection failed", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	repos := repository.New(pool)

	app := fiber.New(fiber.Config{
		AppName:      cfg.App.Name,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		ErrorHandler: errorHandler,
	})

	router.Register(app, cfg, repos)

	go func() {
		addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
		if err := app.Listen(addr, fiber.ListenConfig{DisableStartupMessage: true}); err != nil {
			log.Error("server error", "err", err)
			cancel()
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-ctx.Done():
	case <-quit:
		log.Info("shutdown signal received")
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := app.ShutdownWithContext(shutdownCtx); err != nil {
		log.Error("graceful shutdown failed", "err", err)
	}
	log.Info("server stopped")
}

// errorHandler is the single place that turns any error returned from a handler
// into an API-safe JSON response. It masks non-Fiber errors as a generic 500 to
// ensure internal implementation details never leak to clients.
func errorHandler(c fiber.Ctx, err error) error {
	code := fiber.StatusInternalServerError
	msg := "internal server error"
	var fErr *fiber.Error
	if errors.As(err, &fErr) {
		code = fErr.Code
		msg = fErr.Message
	} else {
		slog.Error("unhandled request error",
			"err", err,
			"method", c.Method(),
			"path", c.Path(),
		)
	}
	return c.Status(code).JSON(models.ErrorResponse{
		ErrorNumber: code,
		Type:        "RequestError",
		Message:     msg,
	})
}
