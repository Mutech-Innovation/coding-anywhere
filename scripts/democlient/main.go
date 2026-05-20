// democlient is a tiny CLI helper that simulates a web client for end-to-end
// demos and manual smoke tests of the relay + daemon stack.
//
// Subcommands:
//
//	democlient claim --relay-url <url> --code <6-digit> [--label <name>]
//	  POST /v1/pair/claim, print the credentials as JSON, exit.
//
//	democlient connect --relay-url <url> --token <jwt> [--for 10s]
//	  Open a WebSocket, fan out a sessions.create request, print everything
//	  the relay sends back, exit when --for elapses.
//
// This is intentionally not part of the daemon or relay binaries — it's a
// diagnostic tool that lives under scripts/ alongside smoke.sh.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Mutech-Innovation/coding-anywhere/internal/pairingclient"
	"github.com/Mutech-Innovation/coding-anywhere/internal/wire"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "claim":
		mustRun(runClaim(os.Args[2:]))
	case "connect":
		mustRun(runConnect(os.Args[2:]))
	case "-h", "--help", "help":
		usage()
	default:
		_, _ = fmt.Fprintf(os.Stderr, "unknown subcommand %q\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func mustRun(err error) {
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func usage() {
	_, _ = fmt.Fprintf(os.Stderr, `democlient — manual smoke-test client for the coding-anywhere relay.

Usage:
  democlient claim   --relay-url <url> --code <6-digit> [--label <name>]
  democlient connect --relay-url <url> --token <jwt> [--for 10s]
`)
}

func runClaim(args []string) error {
	fs := flag.NewFlagSet("claim", flag.ExitOnError)
	relayURL := fs.String("relay-url", "", "Relay base URL")
	code := fs.String("code", "", "6-digit pairing code")
	label := fs.String("label", "demo-client", "Device label")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *relayURL == "" {
		return fmt.Errorf("--relay-url required")
	}
	if len(*code) != 6 {
		return fmt.Errorf("--code must be 6 digits")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	pc := pairingclient.New(*relayURL)
	res, err := pc.Claim(ctx, *code, "client", *label, "")
	if err != nil {
		return fmt.Errorf("claim: %w", err)
	}
	out, err := json.MarshalIndent(res.Credentials, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(out))
	return nil
}

func runConnect(args []string) error {
	fs := flag.NewFlagSet("connect", flag.ExitOnError)
	relayURL := fs.String("relay-url", "", "Relay base URL")
	token := fs.String("token", "", "JWT access token")
	dur := fs.Duration("for", 10*time.Second, "How long to stay connected")
	send := fs.Bool("send-sessions", true, "Send a sessions.create request on connect")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *relayURL == "" || *token == "" {
		return fmt.Errorf("--relay-url and --token are required")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	wsURL := toWS(*relayURL) + "/v1/ws/connect?token=" + *token
	fmt.Printf("→ WS Dial %s\n", strings.Split(wsURL, "?")[0])
	ws, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer func() { _ = ws.Close(websocket.StatusNormalClosure, "") }()
	fmt.Println("✓ ws connected")

	// Reader: print every envelope the server pushes.
	go func() {
		for {
			var env wire.Envelope
			if err := wsjson.Read(ctx, ws, &env); err != nil {
				return
			}
			pretty, _ := json.MarshalIndent(env, "    ", "  ")
			fmt.Printf("← envelope %s.%s\n    %s\n", env.Channel, env.Method, string(pretty))
		}
	}()

	if *send {
		req, err := wire.MakeRequest("demo-1", wire.ChannelSessions, "create",
			map[string]any{"workspace": "/Users/me/coding-anywhere", "agent": "devin"})
		if err != nil {
			return err
		}
		fmt.Printf("→ envelope sessions.create id=%s\n", req.ID)
		if err := wsjson.Write(ctx, ws, req); err != nil {
			return fmt.Errorf("write: %w", err)
		}
	}

	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	deadline := time.After(*dur)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-deadline:
			fmt.Println("\n✓ stay-window elapsed; closing.")
			return nil
		case <-tick.C:
			ping, _ := wire.MakeRequest(fmt.Sprintf("p-%d", time.Now().UnixNano()),
				wire.ChannelMeta, "ping", map[string]string{"nonce": "demo"})
			_ = wsjson.Write(ctx, ws, ping)
		}
	}
}

func toWS(u string) string {
	switch {
	case strings.HasPrefix(u, "http://"):
		return "ws://" + strings.TrimPrefix(u, "http://")
	case strings.HasPrefix(u, "https://"):
		return "wss://" + strings.TrimPrefix(u, "https://")
	}
	return u
}
