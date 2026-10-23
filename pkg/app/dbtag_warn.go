// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"log/slog"
	"strings"

	"github.com/jcsvwinston/nucleus/internal/model"
)

// warnUnknownDBTags emits one WARN per field of meta whose `db:` tag
// carries directives the parser does not recognize. Unknown directives are
// deliberately not an error (they were always ignored, and failing would
// break running apps); the WARN makes the gap visible when the model is
// migrated instead of leaving the developer trusting a constraint that does
// not exist. Since v2.0.0 the framework keeps no model registry
// (DEP-2026-024), so the sweep runs where a model's tags are read at boot:
// AutoMigrate, per model.
func warnUnknownDBTags(logger *slog.Logger, meta *model.ModelMeta) {
	if logger == nil || meta == nil {
		return
	}
	for _, f := range meta.Fields {
		if len(f.UnknownDBTokens) == 0 {
			continue
		}
		// A tag written in Quark's grammar is the common case behind this
		// warning, and naming it saves the reader the guess: Quark's db tag
		// holds the column NAME with comma-separated options
		// (db:"email,size=255"), so every one of its tokens lands here as
		// unrecognized and the field silently keeps the name derived from
		// its Go field instead.
		msg := "model field has unrecognized db tag directives; they have no effect " +
			"(supported: column:<name>, pk, fk:<table.column>, fk:<k=v,…>, index[:name], " +
			"unique[:name], not null, required, readonly, tenant, or \"-\" to exclude the field)"
		if looksLikeQuarkTagGrammar(f.UnknownDBTokens) {
			msg += ". This looks like Quark's db grammar, where the tag holds the column " +
				"name and options are comma-separated: AutoMigrate reads the framework's grammar " +
				"(db:\"column:<name>\", directives separated by semicolons); a Quark model keeps " +
				"its tags and is migrated with Quark's own tooling instead"
		}
		logger.Warn(msg,
			"model", meta.Name, "field", f.Name, "unrecognized", strings.Join(f.UnknownDBTokens, ", "),
		)
	}
}

// looksLikeQuarkTagGrammar reports whether unrecognized db tokens have the
// shape of Quark's tag: a bare column name, or one of its sizing options.
// Quark separates with commas and the framework's grammar with semicolons,
// so a Quark tag arrives here as a single token that is a plain identifier,
// or as one carrying size=/precision=/scale=.
func looksLikeQuarkTagGrammar(tokens []string) bool {
	for _, tok := range tokens {
		t := strings.ToLower(strings.TrimSpace(tok))
		if strings.HasPrefix(t, "size=") || strings.HasPrefix(t, "precision=") || strings.HasPrefix(t, "scale=") {
			return true
		}
		if strings.Contains(t, ",") {
			// A comma is Quark's separator; the framework's grammar never
			// produces one in a token of its own.
			return true
		}
	}
	return false
}
