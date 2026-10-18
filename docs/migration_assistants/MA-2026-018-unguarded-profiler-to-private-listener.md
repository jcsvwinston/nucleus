# Migration Assistant: the profiler a WithoutDefaults() application serves unguarded → WithAuthz(), off, a private listener, or the default stack

- ID: `MA-2026-018`
- Pairs with: `docs/deprecations/DEP-2026-018-unguarded-profiler-without-defaults.md`
- Severity: `high` — an application that carries the combination does not
  start until it is resolved (before v2.0.0 it started, with an ERROR log
  line, and in production heap dumps of the process answered anyone who
  reached the port).
- Status: `current`

---

## Scope

Applications built `WithoutDefaults()` without `WithAuthz()` whose
configuration sets `profiling_enabled: true`. Such an application builds no
RBAC enforcer, so `/debug/pprof` — heap and goroutine dumps included —
would answer anyone who reaches the port. The combination refuses to start
unless `WithAuthz()` guards the profiler (DEP-2026-018).

Out of scope: applications built with the defaults, or `WithoutDefaults()`
with `WithAuthz()`, where the profiler sits behind default-deny, and
applications built `WithoutDefaults()` that leave the profiler off.

## Detection

**Boot — the application refuses to start, and the error says why:**

```
app.New: New profiler: profiler UNGUARDED: profiling_enabled mounts /debug/pprof and this application is built WithoutDefaults() without WithAuthz(), which builds no RBAC enforcer, so no policy can say who may read it — … (prefix=/debug/pprof, env=development) — add WithAuthz() beside WithoutDefaults() … (DEP-2026-018: refused since v2.0.0 unless WithAuthz() guards the profiler)
```

In production the message begins `profiler UNGUARDED in production:` and
names heap dumps of the production process.

**CLI — from the project directory:**

```bash
nucleus doctor --check security   # error: profiling_enabled serves /debug/pprof UNGUARDED … the application refuses to start
```

**Source:**

```bash
grep -nE "WithoutDefaults\(\)|WithAuthz\(\)" main.go cmd/*/main.go 2>/dev/null
grep -n "profiling_enabled" nucleus.yml 2>/dev/null
```

## Rewrite

| Before | After | Kind |
|---|---|---|
| `WithoutDefaults()` + `profiling_enabled: true`, routes that should be authorized against a policy | `WithoutDefaults().WithAuthz()`, and `/debug/pprof/*` granted to an on-call role | manual |
| `WithoutDefaults()` + `profiling_enabled: true`, profiling not needed | `profiling_enabled` removed (off is the default) | manual |
| `WithoutDefaults()` + `profiling_enabled: true`, profiles needed now and then | the key removed, and `net/http/pprof` served from a listener only operators reach | manual |
| `WithoutDefaults()` + `profiling_enabled: true`, the rest of the default stack wanted too | no `WithoutDefaults()`, and `/debug/pprof/*` granted to an on-call role | manual |

A private listener, beside the application's own:

```go
import (
	"log"
	"net/http"
	"net/http/pprof"
)

// Profiles on the loopback interface only: reach them through
// `kubectl port-forward`, an SSH tunnel, or whatever already gives an
// operator a shell next to the process.
go func() {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	log.Println(http.ListenAndServe("127.0.0.1:6060", mux))
}()
```

Register the handlers on a mux of your own, as above: `net/http/pprof`
registers itself on `http.DefaultServeMux` at import time, so a listener
that serves the default mux serves the profiler too — with whatever else
the default mux carries.

With `WithAuthz()`, or on the default stack, a policy row grants the
profiler to the role on call — and every other route answers only to the
callers a policy grants as well:

```csv
p, oncall, /debug/pprof/*, read, allow
```

## Rollback

There is no unguarded state to return to: the choice is `WithAuthz()`, the
profiler off, a private listener, or the default stack (before v2.0.0,
turning `profiling_enabled` back on returned the application to the
unguarded profiler plus the log line).

## Validation

After the rewrite, boot the application and confirm:

1. the application starts — no `profiler UNGUARDED` refusal;
2. `GET /debug/pprof/` on the application's port answers 404 (profiler
   off) or, with `WithAuthz()` or on the default stack, 403 to an anonymous
   caller;
3. `nucleus doctor --check security` reports no profiler finding.
