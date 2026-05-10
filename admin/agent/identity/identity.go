// Package identity resolves the NodeID this agent reports to the admin
// server. It implements the resolution policy decided in the refactor plan
// (decision 15):
//
//   1. UUIDv4 persisted at ${StateDir}/node_id. Stable across restarts.
//   2. If the file cannot be read or written: fallback to
//      "<hostname>-<random8>" and log a WARN.
//
// Resolution is performed once at agent boot. The resulting NodeID is
// constant for the lifetime of the process; the agent never re-reads the
// file. Operators rotating the file see no effect until the next restart,
// which matches the goal of stable identity.
package identity

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// FileName is the path component, relative to the state directory, where
// the NodeID is persisted.
const FileName = "node_id"

// Resolved describes the outcome of NodeID resolution.
type Resolved struct {
	// NodeID is the value the agent will use in NodeRegistration.
	NodeID string

	// Persistent is true when the value came from (or was newly written to)
	// the state directory; false when the value is ephemeral and will not
	// survive a restart.
	Persistent bool

	// Source is a short human-readable label for diagnostics.
	// Values: "loaded", "created", "ephemeral-hostname", "ephemeral-random".
	Source string
}

// Resolver looks up or assigns the NodeID. Use New to construct one.
type Resolver struct {
	stateDir string
	hostname func() (string, error)
	logger   *slog.Logger
}

// New constructs a Resolver. stateDir is the directory under which node_id
// is read/written (typically the value of the framework's state_dir
// configuration key, e.g. "./.nucleus-state"). The logger is used for the
// single WARN we emit on fallback.
func New(stateDir string, logger *slog.Logger) *Resolver {
	if logger == nil {
		logger = slog.Default()
	}
	return &Resolver{
		stateDir: strings.TrimSpace(stateDir),
		hostname: os.Hostname,
		logger:   logger,
	}
}

// Resolve returns the NodeID this process will use. It is idempotent: the
// first call may write a fresh UUIDv4 to disk; subsequent calls return the
// same string by reading the file again.
//
// Resolve never returns an error: if persistence fails, it falls back to
// an ephemeral value and surfaces the failure via the Source field and a
// WARN log line. The agent prefers "running with a less-than-ideal node id"
// over "refusing to start".
func (r *Resolver) Resolve() Resolved {
	if r.stateDir == "" {
		return r.fallback("state directory is not configured")
	}

	path := filepath.Join(r.stateDir, FileName)

	// Try to read existing NodeID.
	if existing, ok := readNodeID(path); ok {
		return Resolved{
			NodeID:     existing,
			Persistent: true,
			Source:     "loaded",
		}
	}

	// Doesn't exist or unreadable: try to create + persist.
	if err := os.MkdirAll(r.stateDir, 0o755); err != nil {
		return r.fallback(fmt.Sprintf("mkdir state_dir %q failed: %v", r.stateDir, err))
	}

	id := uuid.NewString()
	tmpPath := path + ".tmp"
	// Write via tmp + rename to avoid leaving a partial file if the host
	// crashes during the write.
	if err := os.WriteFile(tmpPath, []byte(id+"\n"), 0o600); err != nil {
		return r.fallback(fmt.Sprintf("write node_id failed: %v", err))
	}
	if err := os.Rename(tmpPath, path); err != nil {
		// Best-effort cleanup; the rename is what matters semantically.
		_ = os.Remove(tmpPath)
		return r.fallback(fmt.Sprintf("rename node_id failed: %v", err))
	}

	return Resolved{
		NodeID:     id,
		Persistent: true,
		Source:     "created",
	}
}

func (r *Resolver) fallback(reason string) Resolved {
	hostname := ""
	if r.hostname != nil {
		if h, err := r.hostname(); err == nil {
			hostname = strings.TrimSpace(h)
		}
	}

	suffix, err := randHex(4)
	if err != nil {
		// Truly catastrophic: even crypto/rand failed. Fall back to a
		// caller-distinguishable fixed token. In practice this branch is
		// unreachable on any healthy host.
		suffix = "deadbeef"
	}

	source := "ephemeral-hostname"
	id := hostname
	if id == "" {
		id = "node"
		source = "ephemeral-random"
	}
	id = strings.ReplaceAll(id, " ", "-") + "-" + suffix

	r.logger.Warn(
		"admin agent NodeID falling back to ephemeral identifier",
		"reason", reason,
		"node_id", id,
		"source", source,
	)

	return Resolved{
		NodeID:     id,
		Persistent: false,
		Source:     source,
	}
}

func readNodeID(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			// Read error other than "not exist" is treated as "fall back to
			// fresh write attempt"; the caller will log appropriately if
			// the subsequent write also fails.
		}
		return "", false
	}
	value := strings.TrimSpace(string(data))
	if value == "" {
		return "", false
	}
	// Loose validity check: refuse to use a wildly-formatted value (e.g.
	// the file got corrupted to an HTML page) rather than ship junk in
	// every NodeRegistration.
	if !looksLikeNodeID(value) {
		return "", false
	}
	return value, true
}

func looksLikeNodeID(value string) bool {
	if value == "" || len(value) > 256 {
		return false
	}
	for _, r := range value {
		// Allow alnum, dash, underscore, dot, colon, @, slash. This is a
		// superset of UUID and the ephemeral fallbacks the agent can write.
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9':
			continue
		case r == '-' || r == '_' || r == '.' || r == ':' || r == '@' || r == '/':
			continue
		default:
			return false
		}
	}
	return true
}

func randHex(nbytes int) (string, error) {
	buf := make([]byte, nbytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
