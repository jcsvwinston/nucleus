// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package cli

// `makemigrations` renders the models as they are — the CREATE TABLE of each
// — and a second run would render them again (NU-170). What it can tell
// without a database is which tables the migrations already on disk create:
// those models are left out, and a run that would add nothing writes
// nothing, the way Django answers "No changes detected". A column added to
// a struct whose table is already created stays a migration to write by
// hand: the statements are data, not a schema to diff.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// createTableRE finds the table a CREATE TABLE statement creates, with the
// name bare or quoted the way each engine quotes it ("t", `t`, [t]), after
// an optional IF NOT EXISTS — and after SQL Server's guard, since the
// statement proper follows it.
var createTableRE = regexp.MustCompile("(?i)\\bCREATE\\s+TABLE\\s+(?:IF\\s+NOT\\s+EXISTS\\s+)?[\"`\\[]?([A-Za-z_][A-Za-z0-9_]*)")

// createIndexRE finds the table a CREATE INDEX statement indexes.
var createIndexRE = regexp.MustCompile("(?i)\\bCREATE\\s+(?:UNIQUE\\s+)?INDEX\\s+(?:IF\\s+NOT\\s+EXISTS\\s+)?[\"`\\[]?[A-Za-z_][A-Za-z0-9_]*[\"`\\]]?\\s+ON\\s+[\"`\\[]?([A-Za-z_][A-Za-z0-9_]*)")

// dropTableRE finds the table a DROP TABLE statement drops.
var dropTableRE = regexp.MustCompile("(?i)\\bDROP\\s+TABLE\\s+(?:IF\\s+EXISTS\\s+)?[\"`\\[]?([A-Za-z_][A-Za-z0-9_]*)")

// statementTable names the table an up or down statement is about, lower
// case, or "" when the statement is none of CREATE TABLE, CREATE INDEX or
// DROP TABLE.
func statementTable(stmt string) string {
	for _, re := range []*regexp.Regexp{createTableRE, createIndexRE, dropTableRE} {
		if m := re.FindStringSubmatch(stmt); m != nil {
			return strings.ToLower(m[1])
		}
	}
	return ""
}

// tablesCreatedBy reads every .up.sql under dir and returns the tables they
// create, lower case, each with the file that creates it (the first one, in
// name order). A directory that does not exist creates nothing.
func tablesCreatedBy(dir string) (map[string]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("reading %s: %w", dir, err)
	}
	created := map[string]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".up.sql") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", e.Name(), err)
		}
		for _, m := range createTableRE.FindAllStringSubmatch(string(raw), -1) {
			table := strings.ToLower(m[1])
			if _, seen := created[table]; !seen {
				created[table] = e.Name()
			}
		}
	}
	return created, nil
}

// knownModel is a model left out of the pair: its table is created by a
// migration already on disk.
type knownModel struct {
	Name, Table, File string
}

// withoutKnownTables returns doc without the models whose tables the
// migrations on disk already create — their CREATE TABLE and CREATE INDEX
// statements out of Up, their DROP TABLE out of Down — and the models left
// out, sorted by name.
func withoutKnownTables(doc ddlDocument, created map[string]string) (ddlDocument, []knownModel) {
	if len(created) == 0 {
		return doc, nil
	}
	var known []knownModel
	skip := map[string]bool{}
	kept := doc
	kept.Models = nil
	for _, m := range doc.Models {
		if file, ok := created[strings.ToLower(m.Table)]; ok {
			known = append(known, knownModel{Name: m.Name, Table: m.Table, File: file})
			skip[strings.ToLower(m.Table)] = true
			continue
		}
		kept.Models = append(kept.Models, m)
	}
	if len(known) == 0 {
		return doc, nil
	}
	keep := func(stmts []string) []string {
		out := make([]string, 0, len(stmts))
		for _, s := range stmts {
			if !skip[statementTable(s)] {
				out = append(out, s)
			}
		}
		return out
	}
	kept.Up = keep(doc.Up)
	kept.Down = keep(doc.Down)
	sort.Slice(known, func(i, j int) bool { return known[i].Name < known[j].Name })
	return kept, known
}

// describeKnown is the line that says which models were left out and why.
func describeKnown(known []knownModel) string {
	parts := make([]string, 0, len(known))
	for _, k := range known {
		parts = append(parts, fmt.Sprintf("%s (%s, created by %s)", k.Name, k.Table, k.File))
	}
	return strings.Join(parts, ", ")
}
