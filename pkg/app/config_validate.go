// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"fmt"
	"sort"
	"strings"
)

// validateConfigFileKeys checks the flattened keys loaded from a config FILE
// against the Config schema (koanf tags via ContractConfigKeyPatterns) and
// returns an error naming every unknown key, with a did-you-mean hint when a
// close match exists (DX-13). Before this, the CLI path (app.LoadConfig)
// silently ignored unknown keys: `prot: 9999` reported `overall ok` through
// `nucleus health` while the builder path said `did you mean port?` — the
// same file, two verdicts.
//
// Exempt namespaces, mirroring the builder's loader:
//   - `modules.*` — validated against each module's own struct (ADR-010).
//   - `*_append` / `*_remove` — list-operator keys the builder strips.
func validateConfigFileKeys(loaded map[string]any) error {
	patterns := make([][]string, 0, 160)
	for _, p := range ContractConfigKeyPatterns() {
		patterns = append(patterns, strings.Split(strings.TrimSuffix(p, "[]"), "."))
	}

	var unknown []string
	for key := range loaded {
		if strings.HasPrefix(key, "modules.") ||
			strings.HasSuffix(key, "_append") || strings.HasSuffix(key, "_remove") {
			continue
		}
		if !configKeyMatchesAny(key, patterns) {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)

	var b strings.Builder
	b.WriteString("unknown configuration key(s)")
	for _, k := range unknown {
		b.WriteString("\n  - ")
		b.WriteString(k)
		if hint := configDidYouMean(k); hint != "" {
			b.WriteString(" (did you mean ")
			b.WriteString(hint)
			b.WriteString("?)")
		}
	}
	return fmt.Errorf("%s", b.String())
}

func configKeyMatchesAny(key string, patterns [][]string) bool {
	segs := strings.Split(key, ".")
	for _, pat := range patterns {
		if len(pat) != len(segs) {
			continue
		}
		ok := true
		for i, ps := range pat {
			wildcard := ps == "*" || (strings.HasPrefix(ps, "<") && strings.HasSuffix(ps, ">"))
			if !wildcard && ps != segs[i] {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// configDidYouMean suggests the schema key whose deepest segment is within
// edit distance 3 of the unknown key's deepest segment.
func configDidYouMean(unknown string) string {
	unknownLeaf := unknown
	if i := strings.LastIndex(unknown, "."); i >= 0 {
		unknownLeaf = unknown[i+1:]
	}
	best, bestDist := "", 4
	for _, candidate := range ContractConfigKeyPatterns() {
		c := strings.TrimSuffix(candidate, "[]")
		leaf := c
		if i := strings.LastIndex(c, "."); i >= 0 {
			leaf = c[i+1:]
		}
		if d := editDistance(unknownLeaf, leaf); d < bestDist {
			best, bestDist = c, d
		}
	}
	return best
}

func editDistance(a, b string) int {
	la, lb := len(a), len(b)
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min(min(curr[j-1]+1, prev[j]+1), prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}
