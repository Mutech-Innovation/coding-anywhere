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

	"github.com/Mutech-Innovation/coding-anywhere/internal/version"
)

func main() {
	var (
		showVersion = flag.Bool("version", false, "Print version and exit")
		addr        = flag.String("addr", ":8443", "Listen address")
		logLevel    = flag.String("log-level", "info", "Log level: debug, info, warn, error")
	)
	flag.Parse()

	if *showVersion {
		fmt.Printf("coding-anywhere relay %s\n", version.String())
		return
	}

	logger := newLogger(*logLevel)
	slog.SetDefault(logger)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx, *addr); err != nil {
		slog.Error("relay exited with error", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, addr string) error {
	slog.Info("coding-anywhere relay starting", "version", version.String(), "addr", addr)
	// TODO(P1): HTTP server, WS endpoints, sqlite/badger store.
	<-ctx.Done()
	slog.Info("relay shutting down")
	return nil
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
