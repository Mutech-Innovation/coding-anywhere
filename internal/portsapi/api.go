// Package portsapi exposes detection of locally-listening TCP ports and a
// thin HTTP forwarder for those ports.
//
// The port-detection side uses gopsutil, which is cross-platform on macOS,
// Linux and Windows. The forwarder side is a normal *http.Client with
// reasonable timeouts and a per-host whitelist (only loopback addresses).
package portsapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strconv"
	"time"

	psnet "github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"
)

// PortInfo describes one listening port.
type PortInfo struct {
	Port    uint16 `json:"port"`
	Address string `json:"address"`
	Family  string `json:"family"` // "ipv4" | "ipv6"
	PID     int    `json:"pid,omitempty"`
	Command string `json:"command,omitempty"`
}

// API is the entry point. It is safe for concurrent use.
type API struct {
	httpClient *http.Client
}

// New returns an API with default settings.
func New() *API {
	return &API{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				DialContext:           (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
				ResponseHeaderTimeout: 15 * time.Second,
				IdleConnTimeout:       30 * time.Second,
				MaxIdleConnsPerHost:   8,
			},
		},
	}
}

// ListListening returns the current set of TCP listeners, deduped by port,
// sorted ascending. Only loopback addresses (127.0.0.1, ::1) and 0.0.0.0
// listeners are included — we don't surface ports bound to specific other
// interfaces because they're typically not what the user wants to expose.
func (a *API) ListListening(ctx context.Context) ([]PortInfo, error) {
	conns, err := psnet.ConnectionsWithContext(ctx, "tcp")
	if err != nil {
		return nil, fmt.Errorf("connections: %w", err)
	}
	seen := make(map[uint16]PortInfo)
	for _, c := range conns {
		if c.Status != "LISTEN" {
			continue
		}
		ip := c.Laddr.IP
		if !isLoopbackOrAnyAddr(ip) {
			continue
		}
		if c.Laddr.Port > 65535 {
			continue
		}
		port := uint16(c.Laddr.Port)
		if existing, ok := seen[port]; ok {
			// Prefer the entry whose listener is loopback (127.* / ::1) over a
			// generic "0.0.0.0" entry — typically more useful to the user.
			if isLoopbackAddr(existing.Address) {
				continue
			}
		}
		family := "ipv4"
		if c.Family == 30 || c.Family == 10 {
			family = "ipv6"
		}
		info := PortInfo{
			Port:    port,
			Address: ip,
			Family:  family,
			PID:     int(c.Pid),
		}
		if c.Pid > 0 {
			if p, err := process.NewProcessWithContext(ctx, c.Pid); err == nil {
				if name, err := p.NameWithContext(ctx); err == nil {
					info.Command = name
				}
			}
		}
		seen[port] = info
	}
	out := make([]PortInfo, 0, len(seen))
	for _, p := range seen {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Port < out[j].Port })
	return out, nil
}

// FetchRequest captures everything needed to perform an HTTP request against
// a localhost port.
type FetchRequest struct {
	Port    uint16
	Method  string
	Path    string // includes leading "/" and any query
	Headers http.Header
	Body    []byte
	// Family is "ipv4" or "ipv6". Defaults to "ipv4" if empty.
	Family string
	// MaxBodyBytes caps the inbound response body. Defaults to 4 MiB.
	MaxBodyBytes int64
}

// FetchResponse is the result of forwarding an HTTP request.
type FetchResponse struct {
	Status     int
	Headers    http.Header
	Body       []byte
	Truncated  bool
	UpstreamMS int64
}

// Fetch makes a request against http://<localhost>:<port><path>.
func (a *API) Fetch(ctx context.Context, req FetchRequest) (*FetchResponse, error) {
	if req.Port == 0 {
		return nil, errors.New("port required")
	}
	host := "127.0.0.1"
	if req.Family == "ipv6" {
		host = "[::1]"
	}
	if req.Method == "" {
		req.Method = http.MethodGet
	}
	if req.MaxBodyBytes <= 0 {
		req.MaxBodyBytes = 4 << 20
	}
	url := "http://" + host + ":" + strconv.FormatUint(uint64(req.Port), 10) + req.Path
	hreq, err := http.NewRequestWithContext(ctx, req.Method, url, bytes.NewReader(req.Body))
	if err != nil {
		return nil, fmt.Errorf("build req: %w", err)
	}
	for k, vs := range req.Headers {
		// Strip headers that are meaningless on the upstream side.
		switch k {
		case "Host", "Connection", "Te", "Trailer", "Transfer-Encoding",
			"Upgrade", "Proxy-Connection":
			continue
		}
		for _, v := range vs {
			hreq.Header.Add(k, v)
		}
	}
	start := time.Now()
	hres, err := a.httpClient.Do(hreq)
	if err != nil {
		return nil, fmt.Errorf("upstream: %w", err)
	}
	defer func() { _ = hres.Body.Close() }()

	limited := io.LimitReader(hres.Body, req.MaxBodyBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	truncated := int64(len(body)) > req.MaxBodyBytes
	if truncated {
		body = body[:req.MaxBodyBytes]
	}
	return &FetchResponse{
		Status:     hres.StatusCode,
		Headers:    hres.Header.Clone(),
		Body:       body,
		Truncated:  truncated,
		UpstreamMS: time.Since(start).Milliseconds(),
	}, nil
}

func isLoopbackOrAnyAddr(ip string) bool {
	if ip == "" {
		return false
	}
	if isLoopbackAddr(ip) {
		return true
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	return parsed.IsUnspecified()
}

func isLoopbackAddr(ip string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	return parsed.IsLoopback()
}
