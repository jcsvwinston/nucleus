// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package realtime

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// wsGUID is the constant RFC 6455 mixes into the accept key. It is not a
// secret: it exists so that a cache or a proxy cannot accidentally complete a
// handshake it did not understand.
const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// Opcodes. Only the ones a channel needs are handled; anything else is a
// protocol error, which is what closing with 1002 means.
const (
	opContinuation = 0x0
	opText         = 0x1
	opBinary       = 0x2
	opClose        = 0x8
	opPing         = 0x9
	opPong         = 0xA
)

// DefaultMaxMessageBytes bounds an inbound message. A client that announces a
// gigabyte is refused rather than allocated for: the frame header is attacker
// controlled, and this is the one place where believing it costs the process.
const DefaultMaxMessageBytes = 1 << 20 // 1 MiB

// DefaultPingInterval is how often an idle connection is pinged. A peer that
// does not pong within one interval is gone, whatever TCP still believes.
const DefaultPingInterval = 30 * time.Second

// IsWebSocketUpgrade reports a request asking for the protocol switch.
func IsWebSocketUpgrade(r *http.Request) bool {
	if !strings.EqualFold(r.Method, http.MethodGet) {
		return false
	}
	if !headerContainsToken(r.Header, "Connection", "upgrade") {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(r.Header.Get("Upgrade")), "websocket")
}

func headerContainsToken(h http.Header, name, token string) bool {
	for _, value := range h.Values(name) {
		for _, part := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(part), token) {
				return true
			}
		}
	}
	return false
}

// acceptKey is the handshake's proof that the server understood the request.
func acceptKey(clientKey string) string {
	sum := sha1.Sum([]byte(clientKey + wsGUID)) // #nosec G401 -- the RFC specifies SHA-1 here; it is a handshake token, not a credential
	return base64.StdEncoding.EncodeToString(sum[:])
}

// Conn is an accepted WebSocket connection.
type Conn struct {
	raw     net.Conn
	reader  *bufio.Reader
	writeMu chanLock

	maxMessage int64
}

// chanLock is a mutex that can be acquired with a timeout, which a plain
// sync.Mutex cannot: a writer stuck behind a peer that stopped reading must
// not hold the connection for ever.
type chanLock chan struct{}

func newChanLock() chanLock {
	l := make(chanLock, 1)
	l <- struct{}{}
	return l
}

func (l chanLock) lock(timeout time.Duration) bool {
	select {
	case <-l:
		return true
	case <-time.After(timeout):
		return false
	}
}

func (l chanLock) unlock() {
	select {
	case l <- struct{}{}:
	default:
	}
}

// UpgradeConfig configures the handshake.
type UpgradeConfig struct {
	// MaxMessageBytes overrides DefaultMaxMessageBytes.
	MaxMessageBytes int64
	// CheckOrigin decides whether a cross-origin handshake is allowed. Nil
	// means SAME ORIGIN ONLY, which is the safe default: a browser sends
	// cookies with a WebSocket handshake and does not apply CORS to it, so a
	// permissive upgrade is a cross-site request with the user's session on
	// it.
	CheckOrigin func(r *http.Request) bool
}

// Upgrade completes the handshake and returns the connection.
//
// The framework used to contribute a predicate and a hijackable writer, and
// the application owed itself the accept key, the framing and the ping/pong.
// This is that, with the parts that are easy to get wrong — the origin check,
// the message bound, the masking rules — decided here.
func Upgrade(w http.ResponseWriter, r *http.Request, cfg UpgradeConfig) (*Conn, error) {
	if !IsWebSocketUpgrade(r) {
		http.Error(w, "expected a websocket upgrade", http.StatusBadRequest)
		return nil, errors.New("realtime: not a websocket upgrade")
	}
	if version := strings.TrimSpace(r.Header.Get("Sec-WebSocket-Version")); version != "13" {
		w.Header().Set("Sec-WebSocket-Version", "13")
		http.Error(w, "unsupported websocket version", http.StatusUpgradeRequired)
		return nil, fmt.Errorf("realtime: unsupported websocket version %q", version)
	}
	key := strings.TrimSpace(r.Header.Get("Sec-WebSocket-Key"))
	if key == "" {
		http.Error(w, "missing Sec-WebSocket-Key", http.StatusBadRequest)
		return nil, errors.New("realtime: missing Sec-WebSocket-Key")
	}
	if !originAllowed(r, cfg.CheckOrigin) {
		http.Error(w, "origin not allowed", http.StatusForbidden)
		return nil, errors.New("realtime: origin not allowed")
	}

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return nil, errors.New("realtime: the response writer cannot be hijacked")
	}
	conn, brw, err := hijacker.Hijack()
	if err != nil {
		return nil, fmt.Errorf("realtime: hijack: %w", err)
	}

	response := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + acceptKey(key) + "\r\n\r\n"
	if _, err := brw.WriteString(response); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("realtime: write handshake: %w", err)
	}
	if err := brw.Flush(); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("realtime: flush handshake: %w", err)
	}

	maxMessage := cfg.MaxMessageBytes
	if maxMessage <= 0 {
		maxMessage = DefaultMaxMessageBytes
	}
	return &Conn{
		raw:        conn,
		reader:     brw.Reader,
		writeMu:    newChanLock(),
		maxMessage: maxMessage,
	}, nil
}

// originAllowed decides a cross-origin handshake.
func originAllowed(r *http.Request, check func(*http.Request) bool) bool {
	if check != nil {
		return check(r)
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		// Not a browser: no Origin header. Native clients and tests.
		return true
	}
	// Same origin, compared by host. A browser sends the session cookie with
	// the handshake and does not apply CORS to it, so anything looser has to
	// be an explicit decision by the application.
	trimmed := origin
	if i := strings.Index(trimmed, "://"); i >= 0 {
		trimmed = trimmed[i+3:]
	}
	return strings.EqualFold(trimmed, r.Host)
}

// WriteMessage sends one text message.
func (c *Conn) WriteMessage(data []byte) error { return c.write(opText, data) }

// WriteBinary sends one binary message.
func (c *Conn) WriteBinary(data []byte) error { return c.write(opBinary, data) }

// Ping asks the peer to prove it is there.
func (c *Conn) Ping() error { return c.write(opPing, nil) }

// Close sends a close frame and shuts the connection down.
func (c *Conn) Close() error {
	// 1000: normal closure. Best effort — the peer may already be gone, and
	// there is nothing useful to do about it.
	payload := make([]byte, 2)
	binary.BigEndian.PutUint16(payload, 1000)
	_ = c.write(opClose, payload)
	return c.raw.Close()
}

func (c *Conn) write(opcode byte, payload []byte) error {
	if !c.writeMu.lock(5 * time.Second) {
		return errors.New("realtime: timed out waiting to write; the peer is not reading")
	}
	defer c.writeMu.unlock()

	header := make([]byte, 0, 10)
	header = append(header, 0x80|opcode) // FIN set: no fragmentation on the way out
	length := len(payload)
	switch {
	case length < 126:
		header = append(header, byte(length))
	case length <= 0xFFFF:
		header = append(header, 126, byte(length>>8), byte(length))
	default:
		header = append(header, 127)
		var ext [8]byte
		binary.BigEndian.PutUint64(ext[:], uint64(length))
		header = append(header, ext[:]...)
	}
	// A server MUST NOT mask what it sends (RFC 6455 §5.1).
	if err := c.raw.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}
	if _, err := c.raw.Write(header); err != nil {
		return err
	}
	if len(payload) > 0 {
		if _, err := c.raw.Write(payload); err != nil {
			return err
		}
	}
	return nil
}

// ReadMessage returns the next text or binary message, answering pings and
// handling close frames on the way.
//
// Fragmented messages are reassembled, bounded by the configured maximum: a
// peer that sends an endless stream of continuation frames is refused rather
// than allowed to grow the process.
func (c *Conn) ReadMessage() (opcode byte, payload []byte, err error) {
	var assembled []byte
	var messageOp byte

	for {
		frameOp, fin, masked, data, err := c.readFrame()
		if err != nil {
			return 0, nil, err
		}
		switch frameOp {
		case opPing:
			if err := c.write(opPong, data); err != nil {
				return 0, nil, err
			}
			continue
		case opPong:
			continue
		case opClose:
			_ = c.raw.Close()
			return opClose, data, io.EOF
		}
		if !masked {
			// Every frame from a client MUST be masked (RFC 6455 §5.1). An
			// unmasked one is either a broken client or something pretending
			// to be one.
			_ = c.closeWith(1002, "client frames must be masked")
			return 0, nil, errors.New("realtime: unmasked frame from client")
		}
		if frameOp != opContinuation {
			messageOp = frameOp
			assembled = data
		} else {
			assembled = append(assembled, data...)
		}
		if int64(len(assembled)) > c.maxMessage {
			_ = c.closeWith(1009, "message too big")
			return 0, nil, fmt.Errorf("realtime: message exceeds %d bytes", c.maxMessage)
		}
		if fin {
			return messageOp, assembled, nil
		}
	}
}

func (c *Conn) readFrame() (opcode byte, fin, masked bool, payload []byte, err error) {
	var header [2]byte
	if _, err := io.ReadFull(c.reader, header[:]); err != nil {
		return 0, false, false, nil, err
	}
	fin = header[0]&0x80 != 0
	opcode = header[0] & 0x0F
	masked = header[1]&0x80 != 0
	length := int64(header[1] & 0x7F)

	switch length {
	case 126:
		var ext [2]byte
		if _, err := io.ReadFull(c.reader, ext[:]); err != nil {
			return 0, false, false, nil, err
		}
		length = int64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err := io.ReadFull(c.reader, ext[:]); err != nil {
			return 0, false, false, nil, err
		}
		size := binary.BigEndian.Uint64(ext[:])
		// The length is attacker controlled: refuse before allocating.
		if size > uint64(c.maxMessage) {
			_ = c.closeWith(1009, "message too big")
			return 0, false, false, nil, fmt.Errorf("realtime: frame announces %d bytes, over the %d limit", size, c.maxMessage)
		}
		length = int64(size)
	}
	if length > c.maxMessage {
		_ = c.closeWith(1009, "message too big")
		return 0, false, false, nil, fmt.Errorf("realtime: frame of %d bytes is over the %d limit", length, c.maxMessage)
	}

	var maskKey [4]byte
	if masked {
		if _, err := io.ReadFull(c.reader, maskKey[:]); err != nil {
			return 0, false, false, nil, err
		}
	}
	payload = make([]byte, length)
	if length > 0 {
		if _, err := io.ReadFull(c.reader, payload); err != nil {
			return 0, false, false, nil, err
		}
	}
	if masked {
		for i := range payload {
			payload[i] ^= maskKey[i%4]
		}
	}
	return opcode, fin, masked, payload, nil
}

func (c *Conn) closeWith(code uint16, reason string) error {
	payload := make([]byte, 2, 2+len(reason))
	binary.BigEndian.PutUint16(payload, code)
	payload = append(payload, reason...)
	_ = c.write(opClose, payload)
	return c.raw.Close()
}

// SetReadDeadline bounds how long a read waits, so a connection whose peer
// vanished without a FIN does not hold a goroutine for ever.
func (c *Conn) SetReadDeadline(t time.Time) error { return c.raw.SetReadDeadline(t) }

// RemoteAddr is who is on the other end.
func (c *Conn) RemoteAddr() net.Addr { return c.raw.RemoteAddr() }

// maskKeyForClient generates a mask, for the test client below and for
// anything that needs to speak as a client.
func maskKeyForClient() [4]byte {
	var key [4]byte
	_, _ = rand.Read(key[:])
	return key
}
