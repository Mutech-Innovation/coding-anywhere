// Package main is the entry point for the coding-anywhere relay.
//
// The relay is a small Go service that brokers WebSocket connections between
// daemons running on developer machines and clients (web, mobile). It handles
// device pairing, authentication (JWT), and message routing. It does not
// store conversation content; messages are routed in-memory between an
// account's connected devices.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/Mutech-Innovation/coding-anywhere/apps/relay/internal/server"
	"github.com/Mutech-Innovation/coding-anywhere/internal/version"
)

// devSecret is used when CA_RELAY_JWT_SECRET isn't set. The relay logs a loud
// warning at startup so it cannot be confused with a production deployment.
const devSecret = "dev-secret-do-not-use-in-production-0000000000000000"

func main() {
	var (
		showVersion = flag.Bool("version", false, "Print version and exit")
		addr        = flag.String("addr", "", "Listen address (overrides CA_LISTEN_ADDR)")
		logLevel    = flag.String("log-level", "info", "Log level: debug, info, warn, error")
	)
	flag.Parse()

	if *showVersion {
		fmt.Printf("coding-anywhere relay %s\n", version.String())
		return
	}

	logger := newLogger(*logLevel)
	slog.SetDefault(logger)

	cfg, err := loadConfig(*addr, logger)
	if err != nil {
		logger.Error("config invalid", "err", err)
		os.Exit(2)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	srv, err := server.New(cfg, logger)
	if err != nil {
		logger.Error("server init failed", "err", err)
		os.Exit(1)
	}
	logger.Info("coding-anywhere relay starting",
		"version", version.String(),
		"addr", cfg.ListenAddr,
		"data_dir", cfg.DataDir,
	)
	if err := srv.Run(ctx); err != nil {
		logger.Error("relay exited with error", "err", err)
		os.Exit(1)
	}
	logger.Info("relay shut down")
}

func loadConfig(addrFlag string, logger *slog.Logger) (server.Config, error) {
	if os.Getenv("CA_RELAY_JWT_SECRET") == "" {
		logger.Warn(
			"CA_RELAY_JWT_SECRET not set; using insecure dev default. " +
				"Set this env var (e.g. via docker/relay/.env) before deploying.")
		_ = os.Setenv("CA_RELAY_JWT_SECRET", devSecret)
	}
	cfg, err := server.FromEnv()
	if err != nil {
		return cfg, err
	}
	if addrFlag != "" {
		cfg.ListenAddr = addrFlag
	}
	return cfg, nil
}

func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl}))
}
