// Package i18n is the runtime counterpart of the `nucleus makemessages` /
// `nucleus compilemessages` CLI pair. The CLI extracts translatable strings
// into `.po` catalogs and compiles them into JSON bundles under
// `<locales_path>/<locale>/LC_MESSAGES/<domain>.json`; this package loads
// those bundles, negotiates the request locale from `Accept-Language`, and
// resolves message keys to translated strings with a deterministic fallback
// chain (requested locale → its base language → the default locale → the key
// itself).
//
// Lifecycle: experimental (see docs/reference/API_CONTRACT_INVENTORY.md).
// The surface may still grow (plural forms, per-domain lookup) before it
// freezes. Pure stdlib.
package i18n

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// compiledBundle mirrors the JSON document `nucleus compilemessages` writes:
// {"locale": "...", "domain": "...", "entries": {"key": "translation"}}.
type compiledBundle struct {
	Locale  string            `json:"locale"`
	Domain  string            `json:"domain"`
	Entries map[string]string `json:"entries"`
}

// Catalog holds the compiled message entries for every discovered locale.
// Lookup keys are message IDs as extracted by makemessages; values are the
// translated strings from the compiled bundles.
type Catalog struct {
	// entries maps normalized locale → message key → translation.
	entries map[string]map[string]string
	// names maps normalized locale → the locale name as found on disk,
	// preserved for Locales() and the Content-Language response header.
	names map[string]string
}

// normalizeLocale canonicalises a locale tag for comparison: lower-case,
// with the gettext-style underscore separator folded to the BCP 47 hyphen
// (`es_MX` and `es-mx` both normalize to `es-mx`).
func normalizeLocale(locale string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(locale), "_", "-"))
}

// baseLocale returns the primary language subtag of a normalized locale
// ("es-mx" → "es"). It returns the input unchanged when there is no subtag.
func baseLocale(normalized string) string {
	if idx := strings.IndexByte(normalized, '-'); idx > 0 {
		return normalized[:idx]
	}
	return normalized
}

// Load reads every compiled bundle under dir, the locales root that
// `nucleus compilemessages` writes to. The expected layout is the gettext
// convention the CLI produces:
//
//	<dir>/<locale>/LC_MESSAGES/<domain>.json
//
// All domains found for a locale are merged into one lookup table (in
// lexical file order, so a key defined in two domains resolves to the later
// domain's value — deterministically). A missing dir, or a dir with no
// compiled bundles, yields an empty catalog and no error: an application
// without translations is not misconfigured. A bundle that exists but does
// not parse IS an error — a corrupt catalog should fail loudly at startup,
// not fall back to untranslated strings in production.
func Load(dir string) (*Catalog, error) {
	c := &Catalog{
		entries: make(map[string]map[string]string),
		names:   make(map[string]string),
	}

	dir = strings.TrimSpace(dir)
	if dir == "" {
		return c, nil
	}
	localeDirs, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return c, nil
		}
		return nil, fmt.Errorf("i18n.Load: read locales dir %s: %w", dir, err)
	}

	for _, localeDir := range localeDirs {
		if !localeDir.IsDir() {
			continue
		}
		locale := localeDir.Name()
		messagesDir := filepath.Join(dir, locale, "LC_MESSAGES")
		bundles, err := filepath.Glob(filepath.Join(messagesDir, "*.json"))
		if err != nil {
			return nil, fmt.Errorf("i18n.Load: scan %s: %w", messagesDir, err)
		}
		sort.Strings(bundles)
		for _, bundlePath := range bundles {
			raw, err := os.ReadFile(bundlePath)
			if err != nil {
				return nil, fmt.Errorf("i18n.Load: read bundle %s: %w", bundlePath, err)
			}
			var bundle compiledBundle
			if err := json.Unmarshal(raw, &bundle); err != nil {
				return nil, fmt.Errorf("i18n.Load: parse bundle %s (run `nucleus compilemessages` to regenerate): %w", bundlePath, err)
			}
			normalized := normalizeLocale(locale)
			if normalized == "" {
				continue
			}
			table := c.entries[normalized]
			if table == nil {
				table = make(map[string]string, len(bundle.Entries))
				c.entries[normalized] = table
				c.names[normalized] = locale
			}
			for key, value := range bundle.Entries {
				if strings.TrimSpace(key) == "" {
					continue
				}
				table[key] = value
			}
		}
	}
	return c, nil
}

// Locales returns the locale names found on disk, sorted.
func (c *Catalog) Locales() []string {
	if c == nil {
		return nil
	}
	out := make([]string, 0, len(c.names))
	for _, name := range c.names {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Lookup returns the translation for key in the given locale (compared
// case-insensitively, `_`/`-` folded). The second return reports whether the
// locale has an entry for the key.
func (c *Catalog) Lookup(locale, key string) (string, bool) {
	if c == nil {
		return "", false
	}
	table, ok := c.entries[normalizeLocale(locale)]
	if !ok {
		return "", false
	}
	value, ok := table[key]
	return value, ok
}

// hasLocale reports whether the catalog holds entries for the normalized locale.
func (c *Catalog) hasLocale(normalized string) bool {
	if c == nil {
		return false
	}
	_, ok := c.entries[normalized]
	return ok
}

// Translator resolves message keys against a Catalog with a fixed default
// locale. It is safe for concurrent use: the catalog is read-only after Load.
type Translator struct {
	catalog       *Catalog
	defaultLocale string
}

// New builds a Translator over catalog. defaultLocale is the fallback when a
// requested locale has no catalog or no entry for a key; an empty
// defaultLocale disables that middle step of the fallback chain (the key
// itself is always the final fallback).
func New(catalog *Catalog, defaultLocale string) *Translator {
	if catalog == nil {
		catalog = &Catalog{
			entries: make(map[string]map[string]string),
			names:   make(map[string]string),
		}
	}
	return &Translator{
		catalog:       catalog,
		defaultLocale: normalizeLocale(defaultLocale),
	}
}

// DefaultLocale returns the configured fallback locale, normalized.
func (t *Translator) DefaultLocale() string {
	if t == nil {
		return ""
	}
	return t.defaultLocale
}

// Locales returns the locale names the underlying catalog was loaded with.
func (t *Translator) Locales() []string {
	if t == nil {
		return nil
	}
	return t.catalog.Locales()
}

// T resolves key for locale. The fallback chain is: the requested locale,
// its base language (`es-MX` → `es`), the default locale, and finally the
// key itself — so T always returns a usable string. When args are given the
// resolved string is treated as a fmt format.
func (t *Translator) T(locale, key string, args ...any) string {
	resolved := key
	if t != nil {
		normalized := normalizeLocale(locale)
		if value, ok := t.catalog.Lookup(normalized, key); ok {
			resolved = value
		} else if value, ok := t.catalog.Lookup(baseLocale(normalized), key); ok {
			resolved = value
		} else if value, ok := t.catalog.Lookup(t.defaultLocale, key); ok {
			resolved = value
		}
	}
	if len(args) > 0 {
		return fmt.Sprintf(resolved, args...)
	}
	return resolved
}
