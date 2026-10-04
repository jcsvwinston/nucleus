// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Package catalogbench is the numerator of the A11 gate: what an application
// gets from Nucleus when it EXTENDS itself — the catalog of official modules
// `nucleus add` installs, and the plugins an application can write and run.
// Every control is a probe that runs the real CLI, scaffolds a real project,
// builds and boots it, or drives a real plugin binary, and the test asserts
// the RECORDED verdict rather than success, so closing a gap turns the suite
// red asking for the number to move.
package catalogbench

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
)

type verdict string

const (
	present verdict = "present"
	partial verdict = "partial"
	absent  verdict = "absent"
)

type control struct {
	id     string  // stable id, referenced by the arc plan and the registry
	family string  // grouping for the summary table
	title  string  // what the control is, in one line
	want   verdict // the RECORDED verdict — what the bench publishes
	note   string  // for partial/absent: what exactly is missing
	probe  func(t *testing.T, e *env) verdict
}

func TestCatalogBench(t *testing.T) {
	cases := controls()
	seen := map[string]bool{}
	for _, c := range cases {
		if seen[c.id] {
			t.Fatalf("duplicate control id %q", c.id)
		}
		seen[c.id] = true
		if c.probe == nil {
			t.Fatalf("%s has no probe: a control that cannot be measured does not belong in the bench", c.id)
		}
		if c.want != present && c.note == "" {
			t.Fatalf("%s is %s with no note: the gap has to say what is missing", c.id, c.want)
		}
	}
	// The probes run the Go toolchain on projects outside this module — the
	// CLI's own `go get` included — so a workspace or GOFLAGS inherited from
	// the caller must not decide what they measure.
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "")
	e := newEnv(t)
	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
			got := c.probe(t, e)
			if got == c.want {
				return
			}
			t.Errorf("control %s (%s) measures %q, the bench records %q.\n\n"+
				"If the control just gained ground, that is the point: update the\n"+
				"recorded verdict in catalogbench_cases_test.go in the same change, so\n"+
				"the published numerator moves with the code instead of behind it.",
				c.id, c.title, got, c.want)
		})
	}
}

// TestCatalogBenchSummary prints the per-family counts.
func TestCatalogBenchSummary(t *testing.T) {
	t.Log("\n" + summary(controls()))
}

// TestCatalogBenchTable writes the markdown docs/catalog-bench.md publishes —
// the per-family summary and the catalogue — so the page is generated from
// the cases instead of retyped from them. It only writes when asked:
//
//	NUCLEUS_CATALOG_BENCH_TABLE=1 go test ./internal/catalogbench/ -run TestCatalogBenchTable
func TestCatalogBenchTable(t *testing.T) {
	if os.Getenv("NUCLEUS_CATALOG_BENCH_TABLE") == "" {
		t.Skip("set NUCLEUS_CATALOG_BENCH_TABLE=1 to regenerate the published table")
	}
	cases := controls()
	byFamily, families := grouped(cases)
	var b strings.Builder
	total := map[verdict]int{}
	for _, f := range families {
		rows := byFamily[f]
		sort.Slice(rows, func(i, j int) bool { return rows[i].id < rows[j].id })
		count := map[verdict]int{}
		for _, c := range rows {
			count[c.want]++
			total[c.want]++
		}
		b.WriteString(fmt.Sprintf("\n### %s — %d present · %d partial · %d absent\n\n",
			f, count[present], count[partial], count[absent]))
		b.WriteString("| id | control | verdict | what is missing |\n|---|---|---|---|\n")
		for _, c := range rows {
			note := c.note
			if note == "" {
				note = "—"
			}
			b.WriteString(fmt.Sprintf("| `%s` | %s | **%s** | %s |\n", c.id, mdEscape(c.title), c.want, mdEscape(note)))
		}
	}
	header := fmt.Sprintf("**%d of %d controls present. %d partial. %d absent.**\n",
		total[present], len(cases), total[partial], total[absent])
	var sum strings.Builder
	sum.WriteString("\n| family | present | partial | absent |\n|---|---|---|---|\n")
	for _, f := range families {
		count := map[verdict]int{}
		for _, c := range byFamily[f] {
			count[c.want]++
		}
		sum.WriteString(fmt.Sprintf("| %s | %d | %d | %d |\n", f, count[present], count[partial], count[absent]))
	}
	sum.WriteString(fmt.Sprintf("| **total** | **%d** | **%d** | **%d** |\n", total[present], total[partial], total[absent]))
	if err := os.WriteFile("bench-table.md", []byte(header+sum.String()+b.String()), 0o644); err != nil {
		t.Fatalf("write the table: %v", err)
	}
	t.Logf("wrote bench-table.md: %s", strings.TrimSpace(header))
}

func grouped(cases []control) (map[string][]control, []string) {
	byFamily := map[string][]control{}
	for _, c := range cases {
		byFamily[c.family] = append(byFamily[c.family], c)
	}
	families := make([]string, 0, len(byFamily))
	for f := range byFamily {
		families = append(families, f)
	}
	sort.Strings(families)
	return byFamily, families
}

func summary(cases []control) string {
	byFamily, families := grouped(cases)
	var b strings.Builder
	total := map[verdict]int{}
	for _, f := range families {
		count := map[verdict]int{}
		for _, c := range byFamily[f] {
			count[c.want]++
			total[c.want]++
		}
		b.WriteString(fmt.Sprintf("%-12s present %d · partial %d · absent %d\n", f, count[present], count[partial], count[absent]))
	}
	b.WriteString(fmt.Sprintf("%-12s present %d · partial %d · absent %d  (of %d)\n",
		"TOTAL", total[present], total[partial], total[absent], len(cases)))
	return b.String()
}

// mdEscape keeps a pipe inside a cell from ending it.
func mdEscape(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "|", "\\|"), "\n", " ")
}
