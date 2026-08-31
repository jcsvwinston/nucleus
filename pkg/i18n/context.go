package i18n

import (
	"context"
	"fmt"
)

type contextKey string

const (
	localeKey     contextKey = "nucleus_i18n_locale"
	translatorKey contextKey = "nucleus_i18n_translator"
)

// WithLocale returns a context carrying the resolved locale. The Middleware
// calls it for every request; call it directly in non-HTTP code paths (task
// handlers, mail rendering) that want T to resolve for a specific locale.
func WithLocale(ctx context.Context, locale string) context.Context {
	return context.WithValue(ctx, localeKey, normalizeLocale(locale))
}

// Locale returns the locale stored by WithLocale / the Middleware, or ""
// when the context carries none.
func Locale(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	locale, _ := ctx.Value(localeKey).(string)
	return locale
}

// WithTranslator returns a context carrying the translator T reads from.
// The Middleware injects it on every request; inject it manually in
// non-HTTP code paths that call T.
func WithTranslator(ctx context.Context, t *Translator) context.Context {
	return context.WithValue(ctx, translatorKey, t)
}

// TranslatorFromContext returns the translator stored by WithTranslator /
// the Middleware, or nil when the context carries none.
func TranslatorFromContext(ctx context.Context) *Translator {
	if ctx == nil {
		return nil
	}
	t, _ := ctx.Value(translatorKey).(*Translator)
	return t
}

// T resolves key using the translator and locale carried by ctx (both are
// injected by the Middleware, or by WithTranslator/WithLocale). Without a
// translator on the context it degrades to the key itself (fmt-formatted
// when args are given) — the untranslated-application behaviour, never an
// error.
func T(ctx context.Context, key string, args ...any) string {
	if t := TranslatorFromContext(ctx); t != nil {
		return t.T(Locale(ctx), key, args...)
	}
	if len(args) > 0 {
		return fmt.Sprintf(key, args...)
	}
	return key
}
