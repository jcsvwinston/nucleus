# Migration Assistant: the profiler a WithoutDefaults() application serves unguarded → off, a private listener, or the default stack

- ID: `MA-2026-018`
- Pairs with: `docs/deprecations/DEP-2026-018-unguarded-profiler-without-defaults.md`
- Severity: `low` before v2.0.0 for behaviour (an ERROR log line; the
  application behaves as before) — but the exposure it reports is not low:
  in production, heap dumps of the process answer anyone who reaches the
  port. `high` at v2.0.0 for applications that still carry the combination —
  they stop starting until it is resolved.
- Status: `current`

---

## Scope

Applications built `WithoutDefaults()` whose configuration sets
`profiling_enabled: true`. Such an application builds no RBAC enforcer, so
`/debug/pprof` — heap and goroutine dumps included — answers anyone who
reaches the port unless the application's own middleware refuses it. From
v2.0.0 the combination refuses to start unless an explicit opt-in guards the
profiler (DEP-2026-018).

Out of scope: applications built with the defaults, where the profiler sits
behind default-deny, and applications built `WithoutDefaults()` that leave
the profiler off.

## Detection

**Logs — one ERROR line at boot (this release onward):**

```
level=ERROR msg="profiler UNGUARDED: profiling_enabled mounts /debug/pprof and this application is built WithoutDefaults(), which builds no RBAC enforcer, so no policy can say who may read it — …" prefix=/debug/pprof … deprecation="DEP-2026-018: from v2.0.0 this configuration refuses to start unless an explicit opt-in guards the profiler"
```

In production the message begins `profiler UNGUARDED in production:` and
names heap dumps of the production process.

**CLI — from the project directory:**

```bash
nucleus doctor --check security   # error in production, warning elsewhere: profiling_enabled serves /debug/pprof UNGUARDED …
```

**Source:**

```bash
grep -n "WithoutDefaults()" main.go cmd/*/main.go 2>/dev/null
grep -n "profiling_enabled" nucleus.yml 2>/dev/null
```

## Rewrite

| Before | After | Kind |
|---|---|---|
| `WithoutDefaults()` + `profiling_enabled: true`, profiling not needed | `profiling_enabled` removed (off is the default) | manual |
| `WithoutDefaults()` + `profiling_enabled: true`, profiles needed now and then | the key removed, and `net/http/pprof` served from a listener only operators reach | manual |
| `WithoutDefaults()` + `profiling_enabled: true`, routes that should be authorized anyway | no `WithoutDefaults()`, and `/debug/pprof/*` granted to an on-call role | manual |

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

On the default stack instead, a policy row grants the profiler to the role
on call:

```csv
p, oncall, /debug/pprof/*, read, allow
```

## Rollback

- Before v2.0.0: turning `profiling_enabled` back on returns the application
  to the unguarded profiler plus the log line.
- After v2.0.0: there is no unguarded state to return to; the choice is the
  profiler off, a private listener, the default stack, or the opt-in if it
  has landed (DEP-2026-018, Notes).

## Validation

After the rewrite, boot the application and confirm:

1. no `profiler UNGUARDED` line in the boot log;
2. `GET /debug/pprof/` on the application's port answers 404 (profiler
   off) or, on the default stack, 403 to an anonymous caller;
3. `nucleus doctor --check security` reports no profiler finding.
