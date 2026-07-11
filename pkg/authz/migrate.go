package authz

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// CSVMigrationReport summarizes what MigrateCSVPolicyFile did.
type CSVMigrationReport struct {
	// Path is the input file that was migrated.
	Path string
	// PolicyLinesUpgraded counts `p, sub, obj, act` rows that received an
	// `eft` column. Lines already in 4-column form are not counted.
	PolicyLinesUpgraded int
	// PolicyLinesAlreadyMigrated counts `p, sub, obj, act, eft` rows that
	// were left untouched because they already had the effect column.
	PolicyLinesAlreadyMigrated int
	// GroupingLinesPreserved counts `g, ...` rows; grouping policies have
	// no eft column under the default model and are never rewritten.
	GroupingLinesPreserved int
	// BlankOrCommentLines counts blank lines and `#`-prefixed comments
	// preserved verbatim.
	BlankOrCommentLines int
	// Changed reports whether the file on disk was rewritten. False means
	// every policy line was already in the post-migration shape.
	Changed bool
}

// MigrateCSVPolicyFile rewrites a Casbin RBAC CSV policy file in place so
// every `p` row carries an `eft` column compatible with the deny-override
// model introduced alongside ADR-004.
//
// Behaviour, line by line:
//
//   - blank lines and `#` comments are preserved verbatim
//   - `g, user, role` (and any non-`p` ptype) is preserved verbatim
//   - `p, sub, obj, act` (exactly four fields) is rewritten to
//     `p, sub, obj, act, <defaultEffect>`
//   - `p, sub, obj, act, eft` (five or more fields) is preserved verbatim
//
// The function is idempotent: running it twice produces the same file as
// running it once, and it does not rewrite the file if no upgrade is
// needed (Changed=false in that case).
//
// defaultEffect must be either `allow` or `deny`. Empty string is treated
// as `allow` because every existing 3-column policy was conceptually an
// allow rule under the legacy single-effect Casbin model.
//
// The rewrite is atomic: the new content is written to a sibling temp
// file in the same directory and renamed over the original.
func MigrateCSVPolicyFile(path, defaultEffect string) (CSVMigrationReport, error) {
	report := CSVMigrationReport{Path: path}

	effect := strings.ToLower(strings.TrimSpace(defaultEffect))
	if effect == "" {
		effect = effectAllow
	}
	if effect != effectAllow && effect != effectDeny {
		return report, fmt.Errorf("authz.MigrateCSVPolicyFile: defaultEffect must be %q or %q, got %q", effectAllow, effectDeny, defaultEffect)
	}

	src, err := os.Open(path)
	if err != nil {
		return report, fmt.Errorf("authz.MigrateCSVPolicyFile open: %w", err)
	}
	defer src.Close()

	var out strings.Builder
	scanner := bufio.NewScanner(src)
	// Casbin policy CSVs are short; the default buffer is fine. Be explicit
	// anyway so a pathological file (long inline policy) does not silently
	// truncate.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		switch {
		case trimmed == "":
			report.BlankOrCommentLines++
			out.WriteString(line)
			out.WriteByte('\n')
			continue
		case strings.HasPrefix(trimmed, "#"):
			report.BlankOrCommentLines++
			out.WriteString(line)
			out.WriteByte('\n')
			continue
		}

		fields := splitCSVFields(line)
		if len(fields) == 0 {
			report.BlankOrCommentLines++
			out.WriteString(line)
			out.WriteByte('\n')
			continue
		}

		ptype := strings.ToLower(strings.TrimSpace(fields[0]))
		// Only `p` policies (and family: `p2`, `p3`, …) carry an eft column
		// under the default model. Grouping (`g`, `g2`, …) and anything else
		// stays as-is.
		if !strings.HasPrefix(ptype, "p") {
			report.GroupingLinesPreserved++
			out.WriteString(line)
			out.WriteByte('\n')
			continue
		}

		// Policy rows must have ptype + sub + obj + act = 4 fields minimum
		// to upgrade. Anything shorter is malformed for the default model;
		// preserve it verbatim and let Casbin's own loader complain at
		// startup, which is more helpful than rewriting unparseable rows.
		if len(fields) < 4 {
			report.GroupingLinesPreserved++
			out.WriteString(line)
			out.WriteByte('\n')
			continue
		}

		if len(fields) >= 5 {
			report.PolicyLinesAlreadyMigrated++
			out.WriteString(line)
			out.WriteByte('\n')
			continue
		}

		report.PolicyLinesUpgraded++
		report.Changed = true
		out.WriteString(line)
		out.WriteString(", ")
		out.WriteString(effect)
		out.WriteByte('\n')
	}
	if err := scanner.Err(); err != nil {
		return report, fmt.Errorf("authz.MigrateCSVPolicyFile scan: %w", err)
	}

	if !report.Changed {
		return report, nil
	}

	if err := atomicWrite(path, out.String()); err != nil {
		return report, err
	}
	return report, nil
}

// splitCSVFields splits a Casbin policy CSV line on commas, preserving
// per-field trimming so `p, alice, /data, read` and `p,alice,/data,read`
// both produce the same fields. It does not handle quoted commas, which
// the Casbin file-adapter also does not support.
func splitCSVFields(line string) []string {
	if line == "" {
		return nil
	}
	parts := strings.Split(line, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, strings.TrimSpace(p))
	}
	return out
}

func atomicWrite(path, content string) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".policy-migrate-*")
	if err != nil {
		return fmt.Errorf("authz.MigrateCSVPolicyFile create temp: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() {
		if _, statErr := os.Stat(tmpName); statErr == nil {
			_ = os.Remove(tmpName)
		}
	}
	if _, err := io.WriteString(tmp, content); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("authz.MigrateCSVPolicyFile write temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("authz.MigrateCSVPolicyFile sync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("authz.MigrateCSVPolicyFile close temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("authz.MigrateCSVPolicyFile rename: %w", err)
	}
	return nil
}
