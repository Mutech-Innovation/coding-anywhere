// Package main is the entry point for the coding-anywhere daemon.
//
// The daemon runs on a developer's machine, manages PTY sessions and AI
// coding agents (Devin for Terminal, Claude Code, Codex, cursor-agent), and
// exposes them to remote clients (web, mobile) via an outbound WebSocket
// connection to a relay server.
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
		logLevel    = flag.String("log-level", "info", "Log level: debug, info, warn, error")
	)
	flag.Parse()

	if *showVersion {
		fmt.Printf("coding-anywhere daemon %s\n", version.String())
		return
	}

	logger := newLogger(*logLevel)
	slog.SetDefault(logger)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx); err != nil {
		slog.Error("daemon exited with error", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	slog.Info("coding-anywhere daemon starting", "version", version.String())
	// TODO(P1): config load, relay connect, channel registration.
	<-ctx.Done()
	slog.Info("daemon shutting down")
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
