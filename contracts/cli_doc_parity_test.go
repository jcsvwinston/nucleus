package contracts

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/internal/cli"
)

// builtinPseudoCommands are tokens that may legitimately appear after
// `nucleus ` in the website CLI overview without being registered in
// internal/cli/root.go. They are dispatched directly by the root handler
// (see internal/cli/root.go:101 for "help") rather than registered as
// commandSpec entries.
var builtinPseudoCommands = map[string]struct{}{
	"help": {},
}

// TestCLIDocParity_OverviewCommandsExist guards
// `website/docs/cli/overview.md` against drift: every `nucleus <token>`
// reference in the doc must resolve to a primary command in
// internal/cli/root.go or a Django-style alias in internal/cli/aliases.go.
//
// This is the documentation half of the audit recorded in
// docs/audits/2026-05-12-enterprise-readiness.md (discrepancies D1 and
// D2). Without this guard, doc-only fixes for ficticious commands like
// `nucleus i18n extract` or `nucleus contenttype list` could regress
// silently.
func TestCLIDocParity_OverviewCommandsExist(t *testing.T) {
	docPath := filepath.Join("..", "website", "docs", "cli", "overview.md")
	contents, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("read %s: %v", docPath, err)
	}

	known := make(map[string]struct{}, 64)
	for _, name := range cli.ContractPrimaryCommandNames() {
		known[name] = struct{}{}
	}
	for _, name := range cli.ContractAliasCommandNames() {
		known[name] = struct{}{}
	}
	for name := range builtinPseudoCommands {
		known[name] = struct{}{}
	}

	// Match the first identifier-shaped token after "nucleus " inside any
	// inline-code span. Identifier shape: starts with a letter or
	// underscore, then letters / digits / underscores / hyphens.
	re := regexp.MustCompile("`nucleus\\s+([A-Za-z_][A-Za-z0-9_-]*)")

	missing := make(map[string]struct{})
	for _, m := range re.FindAllStringSubmatch(string(contents), -1) {
		token := m[1]
		if _, ok := known[token]; ok {
			continue
		}
		missing[token] = struct{}{}
	}

	if len(missing) == 0 {
		return
	}

	list := make([]string, 0, len(missing))
	for tok := range missing {
		list = append(list, tok)
	}
	sort.Strings(list)
	t.Fatalf(
		"%s references CLI commands not registered in internal/cli/root.go nor present as aliases: %s",
		docPath,
		strings.Join(list, ", "),
	)
}
