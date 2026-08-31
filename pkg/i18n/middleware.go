package i18n

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
)

// acceptedLanguage is one parsed entry of an Accept-Language header.
type acceptedLanguage struct {
	tag string // normalized language tag, or "*"
	q   float64
	pos int // original position, to keep equal-q entries in header order
}

// parseAcceptLanguage parses an Accept-Language header value into language
// tags ordered by descending quality (equal quality keeps header order).
// Malformed entries are skipped; q=0 entries mean "not acceptable" and are
// dropped, per RFC 9110 §12.4.2.
func parseAcceptLanguage(header string) []acceptedLanguage {
	parts := strings.Split(header, ",")
	out := make([]acceptedLanguage, 0, len(parts))
	for i, part := range parts {
		fields := strings.Split(part, ";")
		tag := strings.TrimSpace(fields[0])
		if tag == "" {
			continue
		}
		q := 1.0
		for _, param := range fields[1:] {
			param = strings.TrimSpace(param)
			if !strings.HasPrefix(param, "q=") {
				continue
			}
			parsed, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimPrefix(param, "q=")), 64)
			if err != nil || parsed < 0 || parsed > 1 {
				q = -1 // malformed q: drop the entry
				break
			}
			q = parsed
		}
		if q <= 0 {
			continue
		}
		normalized := tag
		if tag != "*" {
			normalized = normalizeLocale(tag)
		}
		out = append(out, acceptedLanguage{tag: normalized, q: q, pos: i})
	}
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].q != out[b].q {
			return out[a].q > out[b].q
		}
		return out[a].pos < out[b].pos
	})
	return out
}

// Negotiate picks the best available catalog locale for an Accept-Language
// header value. For each accepted tag, in quality order, it tries: an exact
// catalog match, the tag's base language ("es-MX" → "es"), and finally any
// catalog locale sharing the tag's base language ("es" matches a catalog
// that only ships "es-ES"). A `*` tag and an unmatched header both resolve
// to the default locale.
func (t *Translator) Negotiate(acceptLanguage string) string {
	if t == nil {
		return ""
	}
	for _, accepted := range parseAcceptLanguage(acceptLanguage) {
		if accepted.tag == "*" {
			return t.defaultLocale
		}
		if t.catalog.hasLocale(accepted.tag) {
			return accepted.tag
		}
		base := baseLocale(accepted.tag)
		if t.catalog.hasLocale(base) {
			return base
		}
		// Deterministic base-language match: sorted, so "es" resolves to
		// "es-ar" over "es-mx" every time, not per map iteration order.
		normalizedLocales := make([]string, 0, len(t.catalog.entries))
		for normalized := range t.catalog.entries {
			normalizedLocales = append(normalizedLocales, normalized)
		}
		sort.Strings(normalizedLocales)
		for _, normalized := range normalizedLocales {
			if baseLocale(normalized) == base {
				return normalized
			}
		}
	}
	return t.defaultLocale
}

// Middleware returns standard net/http middleware that negotiates the
// request locale from the Accept-Language header (falling back to the
// translator's default locale) and stores both the locale and the
// translator on the request context, where T and router.Context.T read
// them. It also sets the Content-Language response header to the resolved
// locale.
func (t *Translator) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			locale := t.Negotiate(r.Header.Get("Accept-Language"))
			if locale != "" {
				w.Header().Set("Content-Language", locale)
			}
			ctx := WithTranslator(WithLocale(r.Context(), locale), t)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
