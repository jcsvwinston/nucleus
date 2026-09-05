# ADR-032: A packaging move ships in a minor with a guided error; a `!` is a major

- Status: Accepted
- Date: 2026-09-05
- Follows: [ADR-030](ADR-030-cloud-backends-as-modules.md) and
  [ADR-031](ADR-031-drivers-and-exporters-as-modules.md), the two moves this
  record retroactively classifies
- Context in the suite: QADR-0002 (a major in one pillar is a major of the
  whole set), `docs/governance/COMPATIBILITY_SLO.md`

## Context

Nucleus 1.23.0 moved the cloud storage and secrets backends, the five database
drivers and the two telemetry exporters into modules of their own (ADR-030,
ADR-031). An application that used S3 stopped compiling — or, for a driver,
stopped starting — until it added one blank import, which the boot-time error
spells out and `nucleus add` writes.

The release shipped as a **minor**, and its changelog carries a
"⚠ BREAKING CHANGES" heading, because the merge commit was typed `feat!:`
and the version number was then forced back to a minor with `Release-As`.
The maturity audit of 2026-09-03 (NU-21) read the two together as a
contradiction under a compatibility SLO that promises "no rewrites within
v1.x": either it was breaking and should have been 2.0.0, or it was not and
the heading is wrong.

The question this record answers is not whether 1.23.0 was right — it shipped
and every consumer in the suite followed it with one import per backend — but
**which of the two readings is the rule from now on**, so the next move does
not need a `Release-As` and a conversation.

## Decision

1. **A packaging move is not a breaking change** when all of the following
   hold, and it ships in a **minor** under the compatibility statement
   `packaging move with guided error`:
   - no exported signature changes and no behaviour changes for code that
     compiles: the same configuration produces the same runtime;
   - the only edit a consumer makes is adding an import (or running
     `nucleus add <module>`), and the framework names that edit at boot —
     module path, `go get` line, `import _` line — instead of failing
     generically;
   - the moved code stays available at the same version line, as a sibling
     module of the same repository, released from the same commit.

   It is typed `feat(<scope>):` — no `!`, no `BREAKING CHANGE:` footer — and
   the release notes carry the statement and the list of imports.

2. **A `!` or a `BREAKING CHANGE:` footer means a major, without exception.**
   It is not to be corrected afterwards with `Release-As`: under QADR-0002 a
   major in Nucleus is a major of Quark and Orbit too, so the marker is a
   decision for the whole suite and is taken before the merge, not repaired
   after it. The commit lint of the release train rejects a `!` outside a
   branch that is preparing a major.

3. 1.23.0 is classified retroactively as a packaging move under rule 1: its
   changelog section gets the compatibility statement and a pointer here,
   and the "BREAKING CHANGES" heading stays as history of how it was typed.

## Consequences

- `docs/governance/COMPATIBILITY_SLO.md` gains the third compatibility
  statement, with this definition, next to `no breaking changes` and
  `major-only breaking change with migration plan`.
- `CLAUDE.md` states the rule for commit types, so the next move is typed
  right the first time.
- What a packaging move may NOT do is now written: change a default, remove
  a configuration key, or leave a consumer with a generic error. Prometheus
  in 1.23.0 sits on the edge of this — a default `/metrics` that goes quiet
  until the exporter is imported is a behaviour change — and ADR-031 handled
  it with a boot-time warning; a future move with a similar edge documents
  the edge in its own record before shipping.
