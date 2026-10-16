# Migration Assistant: 3-column Casbin policy CSV → 4-column (deny-override model)

- ID: `MA-2026-003`
- Pairs with: `docs/deprecations/DEP-2026-003-casbin-policy-csv-3col-to-4col.md`
- Severity: `high` (silent breaking change — legacy rows become inert, requests get 403 with no parse-time signal)
- Status: `current`

## Since 2026-10-16

Casbin v2.135.0 refuses a three-field `p` row at load time, so the file no
longer loads inertly: `authz.New` fails with `invalid policy rule size:
expected 4, got 3`, followed by a hint that names `MigrateCSVPolicyFile`
and DEP-2026-003. Detection is therefore the boot failure itself; the
rewrite below is unchanged. At `v2.0.0` the paired notice closes as
`removed` with no further code change.

## Scope

Applications that have any of the following:

- An `admin_rbac.csv` (or any file referenced by `Config.AdminRBACPolicyFile`) that contains policy rows in the legacy 3-argument form: `p, sub, obj, act`.
- Custom code that constructs `authz.New(logger, "path/to/csv")` or `authz.NewFromModel(model, logger, "path/to/csv")` with a hand-curated CSV in the legacy form.
- Any external tool that emits Casbin RBAC CSVs (audit trail exporters, role-management UIs, infrastructure-as-code generators).

Out of scope:

- Programmatic callers using `Enforcer.AddPolicy`, `Enforcer.Deny`, `Enforcer.AddRole`. These already auto-stamp the effect column and remain source-compatible.
- Grouping rows (`g, user, role`) and grouping-variant types (`g2`, …). They have no effect column under the default model.

## Detection

Run from the consumer repo root:

```bash
# 1. Locate every CSV that Casbin might load.
find . -path ./node_modules -prune -o \
       -path ./.git -prune -o \
       -name '*.csv' -print 2>/dev/null

# 2. For each candidate, count legacy 3-argument policy rows.
#    A legacy row is "p," followed by exactly three additional fields.
for f in admin_rbac.csv config/admin_rbac.csv rbac/admin_rbac.csv; do
  [ -f "$f" ] || continue
  echo "=== $f ==="
  awk -F, '
    /^[[:space:]]*#/ { next }
    /^[[:space:]]*$/ { next }
    {
      # Strip surrounding whitespace from the first field for the ptype check.
      gsub(/^[[:space:]]+/, "", $1)
      if ($1 == "p" || $1 ~ /^p[0-9]+$/) {
        if (NF == 4) legacy++
        else if (NF >= 5) migrated++
      }
    }
    END {
      print "  legacy 3-arg policy rows  :", legacy+0
      print "  migrated 4-arg policy rows:", migrated+0
    }
  ' "$f"
done
```

A repository is impacted if any file reports `legacy 3-arg policy rows > 0`.

You can also detect from Go code via the migration helper itself:

```go
report, err := authz.MigrateCSVPolicyFile(path, "allow")
if err != nil {
    log.Fatal(err)
}
fmt.Printf("upgraded=%d already=%d grouping=%d blank/comment=%d changed=%v\n",
    report.PolicyLinesUpgraded,
    report.PolicyLinesAlreadyMigrated,
    report.GroupingLinesPreserved,
    report.BlankOrCommentLines,
    report.Changed,
)
```

`Changed=true` is the definitive signal that a rewrite happened.

## Rewrite Plan

Two paths. Both are safe in a fresh clone; both produce the same content.

### Path A — automated, via the helper

The framework ships `authz.MigrateCSVPolicyFile(path, defaultEffect string) (CSVMigrationReport, error)` (`pkg/authz/migrate.go`). It:

- preserves blank lines and `#` comments verbatim
- preserves grouping rows (`g, …`, `g2, …`) verbatim
- preserves already-migrated 4-argument policy rows verbatim
- rewrites every 3-argument `p, …` row by appending `, <defaultEffect>` (default `allow`)
- writes atomically (tempfile + rename in the same directory)
- returns `Changed=false` if no rewrite was needed (idempotent)

Minimal one-shot driver:

```go
// cmd/migrate-rbac/main.go
package main

import (
	"fmt"
	"log"
	"os"

	"github.com/jcsvwinston/nucleus/pkg/authz"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: migrate-rbac <path-to-rbac.csv> [allow|deny]")
	}
	effect := "allow"
	if len(os.Args) >= 3 {
		effect = os.Args[2]
	}
	report, err := authz.MigrateCSVPolicyFile(os.Args[1], effect)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%+v\n", report)
}
```

Run:

```bash
go run ./cmd/migrate-rbac admin_rbac.csv
# or with an explicit deny default (rare)
go run ./cmd/migrate-rbac admin_rbac.csv deny
```

### Path B — manual, via sed-style rewrite

For repositories that prefer not to add a one-shot Go program:

```bash
cp admin_rbac.csv admin_rbac.csv.bak

# Append ", allow" to every `p, ...` row that has exactly 3 args (NF==4 in CSV).
awk -F, -v OFS=, '
  /^[[:space:]]*#/ { print; next }
  /^[[:space:]]*$/ { print; next }
  {
    first = $1; gsub(/^[[:space:]]+/, "", first)
    if ((first == "p" || first ~ /^p[0-9]+$/) && NF == 4) {
      print $0 ", allow"
    } else {
      print
    }
  }
' admin_rbac.csv.bak > admin_rbac.csv
```

Spot-check the diff before deleting the backup.

## Verification

After migration:

```bash
# No legacy rows remain.
awk -F, '
  /^[[:space:]]*#/ { next }
  /^[[:space:]]*$/ { next }
  {
    first = $1; gsub(/^[[:space:]]+/, "", first)
    if ((first == "p" || first ~ /^p[0-9]+$/) && NF == 4) {
      print "LEGACY ROW: " $0
      bad++
    }
  }
  END { exit bad>0 }
' admin_rbac.csv

# Boot a Nucleus app pointed at the migrated CSV. The default-deny middleware
# should now allow exactly the rows in the file.
nucleus health --config nucleus.yml
nucleus doctor --config nucleus.yml
```

For an integration-style verification, exercise the actual enforcement path:

```go
e, err := authz.New(slog.Default(), "admin_rbac.csv")
if err != nil { log.Fatal(err) }
fmt.Println(e.Can("alice", "/data", "read")) // expected: true after migration
```

If `Can` returns `false` for a subject/action that worked pre-PR-#41, the migration was either incomplete or used the wrong default effect (`deny` instead of `allow`).

## Rollback

The migrator overwrites in place. The two viable rollbacks:

1. **Backup file restore.** Recommended workflow is to `cp admin_rbac.csv admin_rbac.csv.bak` before running the migrator. Restore with `mv admin_rbac.csv.bak admin_rbac.csv`. This is the cleanest path.

2. **Strip the eft column.** A short `awk` removes the trailing field on every `p, …` row:

   ```bash
   awk -F, -v OFS=, '
     /^[[:space:]]*#/ { print; next }
     /^[[:space:]]*$/ { print; next }
     {
       first = $1; gsub(/^[[:space:]]+/, "", first)
       if ((first == "p" || first ~ /^p[0-9]+$/) && NF >= 5) {
         NF = 4
       }
       print
     }
   ' admin_rbac.csv > admin_rbac.csv.legacy
   ```

   This is **not a real rollback** — once `App.New` mounts the deny-override model and the default-deny middleware, the legacy rows are inert. The only way to restore legacy single-effect semantics is to pin the framework to a pre-#41 commit.

## Compatibility Notes

- Additive-first: the helper does not delete or reorder rows; it only appends a field. Comments and grouping policies pass through.
- Reproducible in CI: a CI step can run `authz.MigrateCSVPolicyFile(path, "allow")` and fail the job if `Changed` is `true` — that prevents legacy CSVs from reaching `main`.
- Pre-`v1.0` discipline: the deny-override model is `transitional`, so this MA may be revised if the model is further refined before `v1.0`. The migrator's idempotency guarantees that a second pass over a future CSV format does not introduce regressions.
