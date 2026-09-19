// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package jobsbench

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

// benchDialWS performs a WebSocket handshake and returns the connection, the
// status line and the accept key the server sent. The bench dials raw rather
// than using a client library, because what is being measured is whether the
// FRAMEWORK completes the protocol.
func benchDialWS(rawURL string, headers map[string]string) (net.Conn, string, string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, "", "", err
	}
	conn, err := net.DialTimeout("tcp", u.Host, 5*time.Second)
	if err != nil {
		return nil, "", "", err
	}
	var key [16]byte
	if _, err := rand.Read(key[:]); err != nil {
		_ = conn.Close()
		return nil, "", "", err
	}
	path := u.Path
	if path == "" {
		path = "/"
	}
	req := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n"+
		"Sec-WebSocket-Version: 13\r\nSec-WebSocket-Key: %s\r\n",
		path, u.Host, base64.StdEncoding.EncodeToString(key[:]))
	for k, v := range headers {
		req += k + ": " + v + "\r\n"
	}
	req += "\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		_ = conn.Close()
		return nil, "", "", err
	}
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		_ = conn.Close()
		return nil, "", "", err
	}
	reader := bufio.NewReader(conn)
	status, err := reader.ReadString('\n')
	if err != nil {
		_ = conn.Close()
		return nil, "", "", err
	}
	accept := ""
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			break
		}
		if name, value, ok := strings.Cut(trimmed, ":"); ok && strings.EqualFold(strings.TrimSpace(name), "Sec-WebSocket-Accept") {
			accept = strings.TrimSpace(value)
		}
	}
	return conn, strings.TrimSpace(status), accept, nil
}
