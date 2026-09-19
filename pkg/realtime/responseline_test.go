// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package realtime

// responseLine is what the test client reports about a handshake: the status
// line, the accept key the server sent, and the one it should have sent.
type responseLine struct {
	Raw            string
	Accept         string
	ExpectedAccept string
}
