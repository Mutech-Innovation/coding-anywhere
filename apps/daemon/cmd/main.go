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
	"github.com/Mutech-Innovation/coding-anywhere/internal/diffrouter"
	"github.com/Mutech-Innovation/coding-anywhere/internal/filesrouter"
	"github.com/Mutech-Innovation/coding-anywhere/internal/pairingclient"
	"github.com/Mutech-Innovation/coding-anywhere/internal/portsrouter"
	"github.com/Mutech-Innovation/coding-anywhere/internal/relayclient"
	"github.com/Mutech-Innovation/coding-anywhere/internal/terminalrouter"
	"github.com/Mutech-Innovation/coding-anywhere/internal/version"
	"github.com/Mutech-Innovation/coding-anywhere/internal/wire"
)

// defaultAutoPairRelayURL is what we point the in-daemon auto-pair flow at
// when --relay-url is not given on first run. It matches the `make dev`
// relay binding (apps/relay listens on :8443 in dev).
const defaultAutoPairRelayURL = "http://localhost:8443"

func main() {
	if len(os.Args) >= 2 && os.Args[1] == "init" {
		if err := runInit(os.Args[2:]); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "init: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) >= 2 && os.Args[1] == "key" {
		if err := runKey(os.Args[2:]); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "key: %v\n", err)
			os.Exit(1)
		}
		return
	}

	var (
		showVersion = flag.Bool("version", false, "Print version and exit")
		logLevel    = flag.String("log-level", "info", "Log level: debug, info, warn, error")
		credPath    = flag.String("credentials", "", "Path to credentials file (default: $XDG_DATA_HOME/coding-anywhere/credentials.json)")
		relayURL    = flag.String("relay-url", "", "Override the relay URL stored in credentials")
		workspace   = flag.String("workspace", "", "Workspace root for the files API (default: $PWD)")
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

	if err := run(ctx, logger, *credPath, *relayURL, *workspace); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			logger.Info("daemon shutting down")
			return
		}
		logger.Error("daemon exited with error", "err", err)
		os.Exit(1)
	}
}

func usage() {
	_, _ = fmt.Fprintf(flag.CommandLine.Output(), `Usage: coding-anywhere-daemon [flags]
       coding-anywhere-daemon init [flags]
       coding-anywhere-daemon key <show|rotate|revoke> [flags]

Subcommands:
  init             Pair this machine with the relay (interactive, prints a 6-digit code)
  key show         Print the account key stored in the credentials file
  key rotate       Mint a new account key (old one stops working for new logins)
  key revoke       Clear the account key AND drop every session on the account

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

func run(ctx context.Context, logger *slog.Logger, credPath, relayURLOverride, workspace string) error {
	logger.Info("coding-anywhere daemon starting", "version", version.String())

	if credPath == "" {
		p, err := credentials.DefaultPath()
		if err != nil {
			return fmt.Errorf("default credentials path: %w", err)
		}
		credPath = p
	}

	bundle, err := loadOrAutoPair(ctx, logger, credPath, relayURLOverride)
	if err != nil {
		return err
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

	// files: workspace-scoped browse/read.
	if workspace == "" {
		var err error
		workspace, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("workspace cwd: %w", err)
		}
	}

	// chat: AI coding agent conversations (Devin first; Claude Code, Codex,
	// cursor-agent in v2). The Devin adapter spins up `devin acp` lazily on
	// first session create.
	cr := chatrouter.New(cli, logger, nil, workspace)
	defer func() {
		if err := cr.Close(); err != nil {
			logger.Warn("close chat router", "err", err)
		}
	}()

	if _, err := filesrouter.New(cli, logger, workspace); err != nil {
		return fmt.Errorf("filesrouter: %w", err)
	}
	logger.Info("files workspace", "root", workspace)

	// diff: git-backed change list + per-file versions on the `git` channel.
	if _, err := diffrouter.New(cli, logger, workspace); err != nil {
		return fmt.Errorf("diffrouter: %w", err)
	}

	// ports: localhost listener detection + HTTP forwarder.
	portsrouter.New(cli, logger)

	return cli.Run(ctx)
}

// runKey dispatches the `key` sub-commands.
//
// All three sub-commands operate on the daemon's local credentials file. They
// talk to the relay only when needed (rotate, revoke). show is offline.
func runKey(args []string) error {
	if len(args) == 0 {
		return errors.New("subcommand required: show | rotate | revoke")
	}
	sub := args[0]
	fs := flag.NewFlagSet("key "+sub, flag.ExitOnError)
	credPath := fs.String("credentials", "", "Path to credentials file (default: $XDG_DATA_HOME/coding-anywhere/credentials.json)")
	relayURL := fs.String("relay-url", "", "Override the relay URL stored in credentials")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	if *credPath == "" {
		p, err := credentials.DefaultPath()
		if err != nil {
			return fmt.Errorf("default credentials path: %w", err)
		}
		*credPath = p
	}
	bundle, err := credentials.Load(*credPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("no credentials yet at %s — run `coding-anywhere-daemon init` first", *credPath)
		}
		return fmt.Errorf("load credentials: %w", err)
	}

	relay := bundle.RelayURL
	if *relayURL != "" {
		relay = *relayURL
	}

	switch sub {
	case "show":
		if bundle.AccountKey == "" {
			fmt.Println("(no account key on file — run `coding-anywhere-daemon key rotate` to mint one)")
			return nil
		}
		fmt.Println(bundle.AccountKey)
		return nil

	case "rotate":
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		pc := pairingclient.New(relay)
		newKey, err := pc.RotateAccountKey(ctx, bundle.AccessToken)
		if err != nil {
			return fmt.Errorf("rotate: %w", err)
		}
		bundle.AccountKey = newKey
		if err := credentials.Save(*credPath, bundle); err != nil {
			return fmt.Errorf("save credentials: %w", err)
		}
		fmt.Println("New account key (save somewhere safe — only shown once):")
		fmt.Println("  " + newKey)
		return nil

	case "revoke":
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		pc := pairingclient.New(relay)
		if err := pc.RevokeAccountKey(ctx, bundle.AccessToken); err != nil {
			return fmt.Errorf("revoke: %w", err)
		}
		// The relay also revoked our refresh token, so the locally stored
		// credentials are now junk. Clear them so the daemon can be re-paired
		// from scratch.
		bundle.AccountKey = ""
		if err := credentials.Clear(*credPath); err != nil {
			return fmt.Errorf("clear credentials: %w", err)
		}
		fmt.Println("Account key revoked and all sessions invalidated.")
		fmt.Println("Re-pair the daemon with `coding-anywhere-daemon init --relay-url <relay>`.")
		return nil

	default:
		return fmt.Errorf("unknown subcommand %q (expected show | rotate | revoke)", sub)
	}
}

// loadOrAutoPair loads credentials from disk; if the file does not exist,
// it transparently runs the pairing flow inline so a fresh `make dev` only
// needs the operator to enter the printed 6-digit code in the browser.
//
// relayURLOverride (from --relay-url) wins when set; otherwise we default to
// the dev relay binding so first-time setup is zero-config.
func loadOrAutoPair(
	ctx context.Context,
	logger *slog.Logger,
	credPath, relayURLOverride string,
) (*credentials.Bundle, error) {
	if bundle, err := credentials.Load(credPath); err == nil {
		return bundle, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("load credentials: %w", err)
	}

	relayURL := relayURLOverride
	if relayURL == "" {
		relayURL = defaultAutoPairRelayURL
	}

	logger.Warn(
		"no credentials found; starting auto-pairing — "+
			"enter the code printed below on the Coding Anywhere web/mobile app",
		"relay_url", relayURL,
		"credentials_path", credPath,
	)

	if err := initcmd.Run(ctx, initcmd.Options{
		RelayURL:        relayURL,
		CredentialsPath: credPath,
		Out:             os.Stdout,
		Logger:          logger,
	}); err != nil {
		return nil, fmt.Errorf("auto-pairing: %w", err)
	}

	bundle, err := credentials.Load(credPath)
	if err != nil {
		return nil, fmt.Errorf("load credentials after pairing: %w", err)
	}
	logger.Info("pairing complete; continuing startup")
	return bundle, nil
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
