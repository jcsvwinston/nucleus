# Nucleus Admin Protocol — Evolution Rules

This document is normative. Any change to `admin/proto/**/*.proto` must comply
with the rules below. CI enforces a subset of them via `buf breaking`; the rest
are reviewer responsibilities.

The admin protocol is a wire contract between three parties that release on
**different cadences**:

* the **agent** — embedded in every Nucleus framework binary, version `N`;
* the **admin server** — standalone process, version `N` or `N±1`;
* the **admin UI** — TypeScript bundle, embedded in the admin server but
  cacheable in browsers, version `N` or `N-1`.

We assume rolling deploys. At any moment in production, agent `N` may talk
to admin server `N+1`, and a UI `N-1` may talk to admin server `N`. The rules
here exist to make that work.

---

## 1. Versioning model

* The proto package is `nucleus.admin.v1`. **A new major version
  (`nucleus.admin.v2`) is the only acceptable way to break wire compatibility.**
  Until v1 is retired, all changes go inside `v1`.
* There is no minor/patch version in the proto. The Go module
  (`admin/proto`) carries the SemVer.
* "Stable" in this context means *wire-stable*: a serialized v1 message
  written by any version of the agent must be parseable by any version of
  the admin server, and vice versa.

## 2. Hard rules (CI-enforced)

`buf breaking --against` runs against `main` on every PR and rejects the
following changes (FILE category):

* renaming a field, message, enum, service, or RPC;
* changing a field's type, label (`repeated`, `optional`), or wire-format
  category;
* removing a field, enum value, or RPC;
* changing a field number;
* changing the `oneof` group a field belongs to;
* changing the package name or `go_package` option;
* removing a service or RPC.

If you need to do any of these things, you almost certainly want to introduce
`v2` instead. Talk to the maintainer first.

## 3. Soft rules (reviewer-enforced)

These are not caught by `buf breaking` but will reject a PR in review.

### 3.1 Never remove — only deprecate

Removing a field, enum value, or RPC is a wire break for any peer that still
sends or expects it. Instead:

1. Mark the field with `[deprecated = true]`.
2. Add a comment: `// Deprecated since vX.Y, removed in vZ. Use <replacement>.`
3. Reserve the field number and name when the deprecation cycle ends:

   ```proto
   message Example {
     reserved 4, 7 to 9;
     reserved "old_field_name";
     // ...
   }
   ```

4. Only **after** every deployed agent and admin server has stopped sending
   the field do you flip the proto to `reserved`. That step is a separate PR.

### 3.2 Field numbers are forever

* Never reuse a field number, even after `reserved`.
* Pick the next free integer when adding a field. Don't try to keep fields
  visually grouped by reordering numbers.
* Ranges 1-15 are 1-byte tags on the wire — reserve them for fields used in
  every message instance (`node_id`, `timestamp`, `oneof body` discriminator).
  Rare or large fields go in 16+.

### 3.3 `oneof` tags are stable

When extending a `oneof body { ... }` (e.g. `Frame.body`, `Event.body`,
`Command.body`):

* Add the new variant **at the end** with the next free number.
* Never reorder existing variants.
* Never move a field into or out of a `oneof` after release. Both are wire
  breaks even though the field number stays the same.

### 3.4 Enums grow at the tail

Add new enum values at the end. Treat the existing zero `_UNSPECIFIED` value
as canonical "unknown". Never delete a value: deprecate it first, then mark
it reserved in a follow-up release.

### 3.5 Forward compatibility for snapshot payloads

`SnapshotResponse.payload_json` and `Snapshot.payload_json` carry JSON, not
proto. This is intentional: snapshot shapes evolve faster than RPC contracts
and the cost of reparsing is paid once per UI request. Schemas for each
`SnapshotType` live in `admin/server/snapshot/SCHEMAS.md`. Adding fields to
those JSON schemas is allowed at any time; renaming or removing them follows
the same deprecate-then-reserve discipline.

## 4. Generated code must be reproducible

* `make proto` regenerates the Go and TypeScript stubs.
* The generated files are committed to the repository.
* CI fails if `make proto` produces a non-empty diff.
* Reasoning: a clean checkout must compile (`go build ./...`, `npm run build`)
  without `buf` installed locally, and breaking changes must be reviewable
  in the PR diff.

## 5. Rolling-update compatibility checklist

Before merging any proto PR, write down (in the PR description) the answer to
each of these questions:

1. *Old agent → new admin server.* Does the new server tolerate frames
   missing the new field/oneof variant? (proto3 default values must be a
   valid no-op.)
2. *New agent → old admin server.* Does the old server tolerate frames
   carrying the new field? (Unknown fields are silently kept by `protojson`
   and ignored by binary parsers — usually fine, but verify if the server
   inspects the frame structurally.)
3. *Old UI → new admin server.* Same question for `ControlService` types.
4. *New UI → old admin server.* Same.

If any answer is "no", the PR needs a bump to `v2`, period.

## 6. Process for `v2`

The day we need a wire break (e.g. removing a deprecated RPC or restructuring
`Frame`):

1. Copy the `nucleus/admin/v1` directory to `nucleus/admin/v2`.
2. Make the breaking change in `v2` only.
3. The admin server registers handlers for **both** versions until every
   agent in the fleet has rolled forward. Both versions are legal on the same
   server port (Connect-RPC routes by service URL; the version is part of the
   path).
4. Once agent fleet is on `v2`, drop `v1` server-side support in a separate
   release. Coordinate with the deprecation cycle in `docs/deprecations/`.

Do not do this lightly. The goal is to never need it.
