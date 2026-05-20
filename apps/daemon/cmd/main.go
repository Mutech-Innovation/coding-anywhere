// Package main is the entry point for the coding-anywhere daemon.
//
// The daemon runs on a developer's machine, manages PTY sessions and AI
// coding agents (Devin for Terminal, Claude Code, Codex, cursor-agent), and
// exposes them to remote clients (web, mobile) via an outbound WebSocket
// connection to a relay server.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/Mutech-Innovation/coding-anywhere/apps/daemon/internal/credentials"
	"github.com/Mutech-Innovation/coding-anywhere/apps/daemon/internal/initcmd"
	"github.com/Mutech-Innovation/coding-anywhere/internal/chatrouter"
	"github.com/Mutech-Innovation/coding-anywhere/internal/relayclient"
	"github.com/Mutech-Innovation/coding-anywhere/internal/terminalrouter"
	"github.com/Mutech-Innovation/coding-anywhere/internal/version"
	"github.com/Mutech-Innovation/coding-anywhere/internal/wire"
)

func main() {
	if len(os.Args) >= 2 && os.Args[1] == "init" {
		if err := runInit(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "init: %v\n", err)
			os.Exit(1)
		}
		return
	}

	var (
		showVersion = flag.Bool("version", false, "Print version and exit")
		logLevel    = flag.String("log-level", "info", "Log level: debug, info, warn, error")
		credPath    = flag.String("credentials", "", "Path to credentials file (default: $XDG_DATA_HOME/coding-anywhere/credentials.json)")
		relayURL    = flag.String("relay-url", "", "Override the relay URL stored in credentials")
	)
	flag.Usage = usage
	flag.Parse()

	if *showVersion {
		fmt.Printf("coding-anywhere daemon %s\n", version.String())
		return
	}

	logger := newLogger(*logLevel)
	slog.SetDefault(logger)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx, logger, *credPath, *relayURL); err != nil {
		logger.Error("daemon exited with error", "err", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(flag.CommandLine.Output(), `Usage: coding-anywhere-daemon [flags]
       coding-anywhere-daemon init [flags]

Subcommands:
  init    Pair this machine with the relay (interactive, prints a 6-digit code)

Flags:
`)
	flag.PrintDefaults()
}

func runInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	relayURL := fs.String("relay-url", "", "Relay URL, e.g. https://relay.example.com or ws://localhost:8443 (required)")
	deviceLabel := fs.String("device-label", "", "Friendly label for this machine (defaults to hostname)")
	credPath := fs.String("credentials", "", "Where to save credentials (default: $XDG_DATA_HOME/coding-anywhere/credentials.json)")
	logLevel := fs.String("log-level", "info", "Log level")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *relayURL == "" {
		return errors.New("--relay-url required")
	}
	logger := newLogger(*logLevel)
	slog.SetDefault(logger)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	return initcmd.Run(ctx, initcmd.Options{
		RelayURL:        *relayURL,
		DeviceLabel:     *deviceLabel,
		CredentialsPath: *credPath,
		Logger:          logger,
		Out:             os.Stdout,
	})
}

func run(ctx context.Context, logger *slog.Logger, credPath, relayURLOverride string) error {
	logger.Info("coding-anywhere daemon starting", "version", version.String())

	if credPath == "" {
		p, err := credentials.DefaultPath()
		if err != nil {
			return fmt.Errorf("default credentials path: %w", err)
		}
		credPath = p
	}

	bundle, err := credentials.Load(credPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			logger.Error(
				"no credentials found; run `coding-anywhere init` to pair this machine",
				"path", credPath,
			)
			return errors.New("no credentials")
		}
		return fmt.Errorf("load credentials: %w", err)
	}

	relayURL := bundle.RelayURL
	if relayURLOverride != "" {
		relayURL = relayURLOverride
	}
	logger.Info("loaded credentials",
		"account_id", bundle.AccountID,
		"device_id", bundle.DeviceID,
		"device_kind", bundle.DeviceKind,
		"relay_url", relayURL,
	)

	cli := relayclient.New(relayclient.Config{
		RelayURL:    relayURL,
		AccessToken: bundle.AccessToken,
	}, logger)

	// Channel handlers. meta is implicit (handled inside the client) but a
	// no-op handler keeps debug logs quiet.
	cli.Handle(wire.ChannelMeta, func(wire.Envelope) error { return nil })

	// terminal: PTY-backed shell sessions exposed to remote clients.
	tr := terminalrouter.New(cli, logger)
	defer func() {
		if err := tr.CloseAll(); err != nil {
			logger.Warn("close terminal sessions", "err", err)
		}
	}()

	// chat: AI coding agent conversations (Devin first; Claude Code, Codex,
	// cursor-agent in v2). The Devin adapter spins up `devin acp` lazily on
	// first session create.
	cr := chatrouter.New(cli, logger, nil)
	defer func() {
		if err := cr.Close(); err != nil {
			logger.Warn("close chat router", "err", err)
		}
	}()

	return cli.Run(ctx)
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
