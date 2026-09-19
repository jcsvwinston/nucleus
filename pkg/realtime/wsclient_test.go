// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package realtime

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"testing"
	"time"
)

// testClient is a minimal WebSocket client: enough to prove the server speaks
// the protocol, and nothing more. It masks what it sends, because a server
// that accepts unmasked client frames is broken and this is how that gets
// noticed.
type testClient struct {
	conn   net.Conn
	reader *bufio.Reader
}

func dialWS(t *testing.T, rawURL string, headers map[string]string) (*testClient, *responseLine) {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	conn, err := net.DialTimeout("tcp", u.Host, 5*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	var key [16]byte
	if _, err := rand.Read(key[:]); err != nil {
		t.Fatal(err)
	}
	encoded := base64.StdEncoding.EncodeToString(key[:])

	path := u.Path
	if path == "" {
		path = "/"
	}
	if u.RawQuery != "" {
		path += "?" + u.RawQuery
	}
	req := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n"+
		"Sec-WebSocket-Version: 13\r\nSec-WebSocket-Key: %s\r\n", path, u.Host, encoded)
	for k, v := range headers {
		req += k + ": " + v + "\r\n"
	}
	req += "\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatalf("write handshake: %v", err)
	}

	reader := bufio.NewReader(conn)
	statusLine, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read status: %v", err)
	}
	line := &responseLine{Raw: strings.TrimSpace(statusLine)}
	// Drain the headers, keeping the accept key.
	for {
		h, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read headers: %v", err)
		}
		trimmed := strings.TrimSpace(h)
		if trimmed == "" {
			break
		}
		if name, value, ok := strings.Cut(trimmed, ":"); ok && strings.EqualFold(strings.TrimSpace(name), "Sec-WebSocket-Accept") {
			line.Accept = strings.TrimSpace(value)
		}
	}
	line.ExpectedAccept = acceptKey(encoded)
	return &testClient{conn: conn, reader: reader}, line
}

func (c *testClient) write(opcode byte, payload []byte) error {
	header := []byte{0x80 | opcode}
	length := len(payload)
	switch {
	case length < 126:
		header = append(header, byte(0x80|length))
	case length <= 0xFFFF:
		header = append(header, 0x80|126, byte(length>>8), byte(length))
	default:
		header = append(header, 0x80|127)
		var ext [8]byte
		binary.BigEndian.PutUint64(ext[:], uint64(length))
		header = append(header, ext[:]...)
	}
	mask := maskKeyForClient()
	header = append(header, mask[:]...)
	masked := make([]byte, len(payload))
	for i := range payload {
		masked[i] = payload[i] ^ mask[i%4]
	}
	if _, err := c.conn.Write(append(header, masked...)); err != nil {
		return err
	}
	return nil
}

// read returns the next frame's opcode and payload, waiting at most limit.
func (c *testClient) read(limit time.Duration) (byte, []byte, error) {
	if err := c.conn.SetReadDeadline(time.Now().Add(limit)); err != nil {
		return 0, nil, err
	}
	var header [2]byte
	if _, err := io.ReadFull(c.reader, header[:]); err != nil {
		return 0, nil, err
	}
	opcode := header[0] & 0x0F
	length := int64(header[1] & 0x7F)
	switch length {
	case 126:
		var ext [2]byte
		if _, err := io.ReadFull(c.reader, ext[:]); err != nil {
			return 0, nil, err
		}
		length = int64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err := io.ReadFull(c.reader, ext[:]); err != nil {
			return 0, nil, err
		}
		length = int64(binary.BigEndian.Uint64(ext[:]))
	}
	payload := make([]byte, length)
	if length > 0 {
		if _, err := io.ReadFull(c.reader, payload); err != nil {
			return 0, nil, err
		}
	}
	// A server must never mask: if it did, this would be wrong on purpose.
	if header[1]&0x80 != 0 {
		return opcode, payload, fmt.Errorf("the server masked a frame, which RFC 6455 forbids")
	}
	return opcode, payload, nil
}
