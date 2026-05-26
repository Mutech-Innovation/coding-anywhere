// Package initcmd implements the `coding-anywhere-daemon init` flow.
//
// The flow:
//
//  1. Hit the relay's POST /v1/pair/init with this machine's label.
//  2. Print the 6-digit code in big letters.
//  3. Poll GET /v1/pair/status until claimed (or expired).
//  4. Persist the resulting credentials to the on-disk credentials file.
//
// `Run` is intentionally a single function so the daemon's main can drop in
// without importing extra plumbing.
package initcmd

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/Mutech-Innovation/coding-anywhere/apps/daemon/internal/credentials"
	"github.com/Mutech-Innovation/coding-anywhere/internal/pairingclient"
)

// Options controls the init command.
type Options struct {
	// RelayURL is the relay base URL (http(s):// or ws(s)://).
	RelayURL string
	// DeviceLabel is what to call this machine in the user's device list.
	DeviceLabel string
	// CredentialsPath defaults to credentials.DefaultPath().
	CredentialsPath string
	// PollInterval is how often to poll status. Defaults to 2s.
	PollInterval time.Duration
	// Out is where the user-facing prompts go. Defaults to os.Stdout.
	Out io.Writer
	// Logger receives structured logs (info, warn). Defaults to slog.Default().
	Logger *slog.Logger
}

// Run executes the init flow.
func Run(ctx context.Context, opts Options) error {
	if opts.RelayURL == "" {
		return fmt.Errorf("relay URL required")
	}
	if strings.TrimSpace(opts.DeviceLabel) == "" {
		host, _ := os.Hostname()
		if host == "" {
			host = "this-machine"
		}
		opts.DeviceLabel = host
	}
	if opts.Out == nil {
		opts.Out = os.Stdout
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.CredentialsPath == "" {
		p, err := credentials.DefaultPath()
		if err != nil {
			return fmt.Errorf("default credentials path: %w", err)
		}
		opts.CredentialsPath = p
	}
	if opts.PollInterval == 0 {
		opts.PollInterval = 2 * time.Second
	}

	pc := pairingclient.New(opts.RelayURL)
	bearer := existingBearer(ctx, pc, opts.CredentialsPath, opts.RelayURL, opts.Logger)

	opts.Logger.Info("starting pairing", "relay_url", opts.RelayURL, "label", opts.DeviceLabel)

	init, err := pc.Init(ctx, "daemon", opts.DeviceLabel, bearer)
	if err != nil {
		return fmt.Errorf("pair init: %w", err)
	}
	printCode(opts.Out, init)

	bundle, err := pc.WaitClaimed(ctx, init.PairingToken, opts.PollInterval)
	if err != nil {
		return fmt.Errorf("waiting for claim: %w", err)
	}

	saved := bundleFromPairing(opts.RelayURL, *bundle)
	if err := credentials.Save(opts.CredentialsPath, saved); err != nil {
		return fmt.Errorf("save credentials: %w", err)
	}

	_, _ = fmt.Fprintf(opts.Out, "\nPaired! Saved credentials to %s\n", opts.CredentialsPath)
	_, _ = fmt.Fprintf(opts.Out, "Account: %s  Device: %s (%s)\n", saved.AccountID, saved.DeviceID, saved.DeviceLabel)
	if saved.AccountKey != "" {
		_, _ = fmt.Fprintf(opts.Out, "\nAccount key (save this somewhere safe — only shown once):\n  %s\n",
			saved.AccountKey)
		_, _ = fmt.Fprintln(opts.Out,
			"Use it on other browsers/phones via the \"sign in with account key\" form to skip the pairing code.")
	}
	return nil
}

func existingBearer(ctx context.Context, pc *pairingclient.Client, path, relayURL string, logger *slog.Logger) string {
	b, err := credentials.Load(path)
	if err != nil {
		if !os.IsNotExist(err) {
			logger.Warn("could not load existing credentials for account-preserving pairing", "err", err)
		}
		return ""
	}
	if strings.TrimRight(b.RelayURL, "/") != strings.TrimRight(relayURL, "/") {
		return ""
	}
	if time.Now().UTC().Before(b.AccessTokenExpiresAt.Add(-time.Minute)) {
		return b.AccessToken
	}
	access, ttl, err := pc.Refresh(ctx, b.RefreshToken)
	if err != nil {
		logger.Warn("could not refresh existing credentials for account-preserving pairing", "err", err)
		return ""
	}
	b.AccessToken = access
	b.AccessTokenExpiresAt = time.Now().UTC().Add(time.Duration(ttl) * time.Second)
	if err := credentials.Save(path, b); err != nil {
		logger.Warn("could not save refreshed credentials", "err", err)
	}
	return access
}

func bundleFromPairing(relayURL string, c pairingclient.CredentialBundle) *credentials.Bundle {
	now := time.Now().UTC()
	return &credentials.Bundle{
		RelayURL:              relayURL,
		AccountID:             c.AccountID,
		DeviceID:              c.DeviceID,
		DeviceKind:            c.DeviceKind,
		DeviceLabel:           c.DeviceLabel,
		AccessToken:           c.AccessToken,
		AccessTokenExpiresAt:  now.Add(time.Duration(c.AccessTokenExpiresIn) * time.Second),
		RefreshToken:          c.RefreshToken,
		RefreshTokenExpiresAt: now.Add(time.Duration(c.RefreshTokenExpiresIn) * time.Second),
		RelayProtocolVersion:  c.RelayProtocolVersion,
		AccountKey:            c.AccountKey,
	}
}

// printCode renders a slightly-pretty code box. Falls back to plain text if
// the writer doesn't look like a TTY (no ANSI assumed).
func printCode(w io.Writer, init *pairingclient.InitResponse) {
	bar := strings.Repeat("─", 16)
	_, _ = fmt.Fprintf(w, "\n┌%s┐\n", bar)
	_, _ = fmt.Fprintf(w, "│   %s   │\n", spaced(init.Code))
	_, _ = fmt.Fprintf(w, "└%s┘\n", bar)
	_, _ = fmt.Fprintf(w, "\nEnter this code on the Coding Anywhere web/mobile app to finish pairing.\n")
	_, _ = fmt.Fprintf(w, "(Code expires in %ds)\n", init.ExpiresIn)
}

func spaced(code string) string {
	if len(code) != 6 {
		return code
	}
	return code[:3] + " " + code[3:]
}
