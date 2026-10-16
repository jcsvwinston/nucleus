# Migration Assistant: the asynq jobs provider from the root module → `providers/tasks-asynq`

- ID: `MA-2026-020`
- Pairs with: `docs/deprecations/DEP-2026-020-asynq-provider-to-its-own-module.md`
- Severity: `low` (packaging only: same symbols, same behaviour; at v2.0.0
  a boot that selects `asynq` without the module fails with the `go get`
  named, like every optional module since ADR-031).
- Status: `current`

---

## Scope

Applications with `jobs_provider: asynq` (or `NUCLEUS_JOBS_PROVIDER=asynq`),
and code that imports `github.com/jcsvwinston/nucleus/pkg/tasks/providers/asynq`
directly.

Out of scope: `jobs_provider: memory` (the default) and `jobs_provider: sql`;
the `nucleus doctor` tasks check, which the CLI's own module keeps linking.

## Detection

```bash
grep -rn 'jobs_provider' nucleus.yml config/ 2>/dev/null
env | grep NUCLEUS_JOBS_PROVIDER
grep -rn 'pkg/tasks/providers/asynq"' --include='*.go' .
```

## Rewrite

| Before | After | Kind |
|---|---|---|
| `jobs_provider: asynq` with the root module only | the same key, plus `nucleus add tasks-asynq` (or `go get github.com/jcsvwinston/nucleus/providers/tasks-asynq@v2` and a blank import in the composition root) | manual (one command) |
| `import ".../pkg/tasks/providers/asynq"` | `import ".../providers/tasks-asynq"` | automatic (`sed`) |

```bash
sed -i '' 's#github.com/jcsvwinston/nucleus/pkg/tasks/providers/asynq#github.com/jcsvwinston/nucleus/providers/tasks-asynq#g' $(grep -rl 'pkg/tasks/providers/asynq' --include='*.go' .)
```

## Verification

```bash
go build ./... && go vet ./...
nucleus doctor --check tasks         # inspects the asynq queues when the provider is linked
```

## Rollback

Before v2.0.0 nothing changes. After it, the module is the only place the
provider lives; a rollback is a downgrade of the framework.
