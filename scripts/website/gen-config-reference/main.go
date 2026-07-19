// Command gen-config-reference renders the public Configuration reference
// page (website/docs/reference/configuration.md) from the configuration key
// registry (docs/reference/CONFIG_KEY_REGISTRY.md).
//
// The registry is the internal source of truth for every configuration key:
// defaults, lifecycle tags, and semantics. The public site must not link to
// it (readers should not need the internal tree), so this generator projects
// the registry's per-section key tables into a Docusaurus page. CI keeps the
// two in lockstep: a freshness step regenerates the page and fails on any
// diff, so editing the registry without re-running the generator — or editing
// the generated page by hand — breaks the build.
//
// What is emitted:
//
//   - A fixed intro (precedence chain, env-var mapping, lifecycle legend)
//     maintained here, not parsed from the registry.
//   - Every `## Section` of the registry that contains at least one markdown
//     table, with its `###`/`####` subheadings and tables. Free prose between
//     tables is NOT carried over — it is written for maintainers and is
//     full of internal references; the durable per-key content lives in the
//     Notes column.
//   - A fixed note for the `modules.*` namespace (that registry section is
//     all prose, but the namespace must not vanish from the public page).
//
// Table cells are sanitized for the public site: parentheticals and clauses
// that reference internal artifacts (decision-record IDs, deprecation or
// migration tickets, internal file names, priority labels) are dropped. The
// generator then re-scans its own output and exits non-zero if any internal
// vocabulary survived, so a new kind of leak fails CI instead of shipping.
//
// Usage: go run ./scripts/website/gen-config-reference [-root .]
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	registryPath = "docs/reference/CONFIG_KEY_REGISTRY.md"
	outputPath   = "website/docs/reference/configuration.md"
)

// skipSections are registry H2 sections whose content is either internal
// process (contract rules) or already covered by the fixed intro.
var skipSections = map[string]bool{
	"Configuration Precedence": true,
	"Lifecycle Tags":           true,
	"Contract Rules":           true,
}

// internalRef matches internal-only vocabulary that must never reach the
// public page: architecture-decision IDs, deprecation/migration ticket IDs,
// internal document names, internal priority/finding labels, and issue
// numbers.
var internalRef = regexp.MustCompile(
	`ADR-[0-9]+|DEP-[0-9]{4}-[0-9]+|MA-[0-9]{4}-[0-9]+|SPEC\.md|SPEC §|CLAUDE\.md|TASKS\.md|V1_GATE|v1 gate|v1\.0-gate|\bNU-?P?[0-9]+(-[0-9]+)?\b|\bP[0-3]\b|\(#[0-9]+\)|§`)

// paren matches an innermost parenthetical (no nested parens inside).
var paren = regexp.MustCompile(`\([^()]*\)`)

func main() {
	root := flag.String("root", ".", "repo root")
	flag.Parse()

	src, err := os.ReadFile(filepath.Join(*root, registryPath))
	if err != nil {
		fatal("read registry: %v", err)
	}

	body := render(string(src))

	if hits := internalRef.FindAllString(body, -1); len(hits) > 0 {
		fatal("internal vocabulary leaked into the generated page (extend the sanitizer): %v", hits)
	}

	out := filepath.Join(*root, outputPath)
	if err := os.WriteFile(out, []byte(body), 0o644); err != nil {
		fatal("write %s: %v", out, err)
	}
	fmt.Printf("gen-config-reference: wrote %s (%d bytes)\n", out, len(body))
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "gen-config-reference: "+format+"\n", args...)
	os.Exit(1)
}

const header = `---
sidebar_position: 2
title: Configuration reference
description: Every configuration key Nucleus recognizes — default, lifecycle, and semantics.
covers: []
config_keys: []
---

{/* GENERATED — edit CONFIG_KEY_REGISTRY.md and re-run go run ./scripts/website/gen-config-reference */}

# Configuration reference

This page lists every configuration key the framework recognizes, with its
default value and lifecycle. For how configuration is loaded and merged —
file formats, the multi-file loader, list operators, module config — see
[Concepts → Configuration](../concepts/configuration.md).

## How to read this page

**Precedence.** Values are resolved lowest-to-highest:

` + "```" + `
struct defaults  <  config file(s)  <  NUCLEUS_* env vars
` + "```" + `

**Environment variables.** Every key maps to a ` + "`NUCLEUS_`" + `-prefixed
variable. Flat keys use one underscore (` + "`port`" + ` →
` + "`NUCLEUS_PORT`" + `); nested keys join segments with a **double**
underscore (` + "`databases.<alias>.url`" + ` →
` + "`NUCLEUS_DATABASES__<ALIAS>__URL`" + `).

**Lifecycle.**

| Tag | Meaning |
| --- | --- |
| ` + "`stable`" + ` | Key name and semantics are contract surfaces on the v1.x line. |
| ` + "`transitional`" + ` | Supported, but semantics may still refine before freezing. |
| ` + "`experimental`" + ` | No compatibility guarantee yet. |
| ` + "`removed`" + ` | No longer accepted; the notes name the replacement. |

To see the value every key actually resolves to in a running deployment —
and which file or variable set it — use
` + "`nucleus config print --effective`" + `
([CLI overview](../cli/overview.md#effective-config-nucleus-config-print---effective)).
`

// modulesNote replaces the registry's prose-only modules.* section.
const modulesNote = `## Module configuration (` + "`modules.*`" + `)

The ` + "`modules.<name>.*`" + ` namespace is reserved for mounted modules.
Each module owns its own schema, declared as struct tags on its typed config —
the framework does not validate those keys against the tables above. Two
practical limits: the ` + "`NUCLEUS_MODULES__*`" + ` env-var pattern is not
applied (module config comes from files or code), and
` + "`nucleus config print --effective`" + ` excludes ` + "`modules.*`" + `
values (module schemas are open-ended and may carry secrets). See
[Concepts → Configuration → Module-specific configuration](../concepts/configuration.md#module-specific-configuration-modules)
for the full authoring guide.
`

func render(registry string) string {
	var b strings.Builder
	b.WriteString(header)

	for _, sec := range splitSections(registry) {
		if skipSections[sec.title] {
			continue
		}
		if strings.HasPrefix(sec.title, "Module-specific configuration") {
			b.WriteString("\n")
			b.WriteString(modulesNote)
			continue
		}
		rendered := renderSection(sec)
		if rendered == "" {
			continue // no tables — nothing durable to publish
		}
		b.WriteString("\n")
		b.WriteString(rendered)
	}
	return b.String()
}

type section struct {
	title string
	lines []string
}

// splitSections cuts the registry at H2 headings, dropping the preamble.
func splitSections(src string) []section {
	var out []section
	var cur *section
	for _, line := range strings.Split(src, "\n") {
		if strings.HasPrefix(line, "## ") {
			out = append(out, section{title: strings.TrimSpace(strings.TrimPrefix(line, "## "))})
			cur = &out[len(out)-1]
			continue
		}
		if cur != nil {
			cur.lines = append(cur.lines, line)
		}
	}
	return out
}

// renderSection emits the section heading, its sub-headings, and its
// sanitized tables. Returns "" when the section holds no table.
func renderSection(sec section) string {
	var b strings.Builder
	b.WriteString("## " + sanitizeCell(sec.title) + "\n")

	var pendingHeading string
	var table []string
	sawTable := false

	flushTable := func() {
		if len(table) == 0 {
			return
		}
		sawTable = true
		if pendingHeading != "" {
			b.WriteString("\n" + pendingHeading + "\n")
			pendingHeading = ""
		}
		// Fixed lead-ins for the two sub-tables whose meaning lived in
		// skipped prose (see the package comment).
		headerRow := table[0]
		switch {
		case strings.HasPrefix(headerRow, "| Reference |"):
			b.WriteString("\nReference forms accepted by `secret_env` and `pem_env` " +
				"(plain names read the environment; the `aws-sm:` scheme reads " +
				"AWS Secrets Manager via the standard credential chain):\n")
		case strings.HasPrefix(headerRow, "| Field |"):
			b.WriteString("\nExactly one of `secret_env` / `pem_path` / `pem_env` " +
				"must be set per entry — key material is never read from tracked " +
				"config files:\n")
		}
		b.WriteString("\n")
		for _, row := range table {
			b.WriteString(sanitizeRow(row) + "\n")
		}
		table = nil
	}

	for _, line := range sec.lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "|"):
			table = append(table, trimmed)
		case strings.HasPrefix(trimmed, "### "), strings.HasPrefix(trimmed, "#### "):
			flushTable()
			pendingHeading = sanitizeCell(trimmed)
		default:
			flushTable() // prose or blank line ends any open table
		}
	}
	flushTable()

	if !sawTable {
		return ""
	}
	return b.String()
}

// sanitizeRow sanitizes every cell of one markdown table row.
func sanitizeRow(row string) string {
	cells := strings.Split(row, "|")
	for i, c := range cells {
		cells[i] = padCell(sanitizeCell(strings.TrimSpace(c)))
	}
	return strings.Join(cells, "|")
}

func padCell(c string) string {
	if c == "" {
		return ""
	}
	return " " + c + " "
}

// sanitizeCell removes internal references from one cell:
//  1. parentheticals containing an internal reference are dropped;
//  2. remaining sentences/clauses containing one are dropped;
//  3. whitespace and dangling punctuation are tidied.
func sanitizeCell(cell string) string {
	// 1. Parentheticals, innermost first, until stable.
	for {
		next := paren.ReplaceAllStringFunc(cell, func(m string) string {
			if internalRef.MatchString(m) {
				return ""
			}
			return m
		})
		if next == cell {
			break
		}
		cell = next
	}

	// 2. Clause / sentence filtering. Cells are single-line. Clauses ("; ")
	// are filtered first so that a sentence keeps its clean clauses when only
	// one clause carries an internal reference.
	cell = filterSeparated(cell, "; ")
	cell = filterSeparated(cell, ". ")

	// 3. Tidy.
	cell = strings.Join(strings.Fields(cell), " ")
	cell = strings.ReplaceAll(cell, " .", ".")
	cell = strings.ReplaceAll(cell, " ,", ",")
	cell = strings.ReplaceAll(cell, "— .", "")
	cell = strings.TrimSpace(cell)
	// A cell that is ONLY an em dash is a deliberate "no value" marker —
	// keep it; otherwise strip a dangling trailing dash left by a removal.
	if cell != "—" {
		cell = strings.TrimSpace(strings.TrimSuffix(cell, "—"))
	}
	return cell
}

func filterSeparated(s, sep string) string {
	if !strings.Contains(s, sep) {
		return s
	}
	parts := strings.Split(s, sep)
	kept := parts[:0]
	for _, p := range parts {
		if internalRef.MatchString(p) {
			continue
		}
		kept = append(kept, p)
	}
	return strings.Join(kept, sep)
}
