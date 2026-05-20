// Package relayproto defines the wire-level routing protocol that connects
// daemons and clients to the relay. It sits below the application channels
// (terminal, chat, files, etc.) and handles framing, ack/retry, and
// multi-device fan-out.
package relayproto
