---
sidebar_position: 3
title: Upgrading
covers: []
config_keys: []
---

# Upgrading

Nucleus follows Semantic Versioning on the stable `v1.x` line: code that
sticks to stable surfaces (public Go API in `pkg/*`, CLI commands and flags,
registered config keys) upgrades between `v1.x` releases without code
changes. What exactly is promised — maturity levels, what counts as
breaking, and the deprecation cycle — is specified in
[Support & compatibility](../architecture/compatibility.md); this page is
the operational recipe.

## Patch and minor upgrades (`v1.x` → `v1.y`)

1. **Read the [release notes](../reference/release-notes.md)** for every
   version you are crossing. Minor releases can *harden defaults* (that is
   an allowed, announced change — v1.2.0 did it for proxy headers and JWT
   secret length), and the "Upgrade notes" section of each release is where
   such changes are called out.

2. **Bump the module and tidy** (pin the exact target version, or use
   `@latest` for the newest release):

   ```bash
   go get github.com/jcsvwinston/nucleus@latest
   go mod tidy
   ```

3. **Upgrade the CLI to the same version** — the CLI and the framework are
   released together, and mixing versions is untested territory:

   ```bash
   go install github.com/jcsvwinston/nucleus/cmd/nucleus@latest
   nucleus --version
   ```

4. **Build and test.** Contract freeze tests on the framework side guard
   against accidental removals, but your own test suite is what verifies
   *your* app's behavior:

   ```bash
   go build ./... && go test ./...
   ```

5. **Run migrations, then deploy.** Framework upgrades do not apply
   anything to your database by themselves; if a release's upgrade notes
   mention schema-affecting changes, they will tell you what to run:

   ```bash
   nucleus migrate --config nucleus.yml status
   nucleus migrate --config nucleus.yml up
   ```

6. **Preflight** the new binary with `nucleus health --deploy` before
   routing traffic ([Deployment](./deployment.md#preflight-checks)).

## Upgrading as part of the Quantum suite

Nucleus is one module of a small suite — alongside `quark` (query builder)
and `orbit` (the admin panel module). Each module releases independently,
so "latest of each" is not automatically a combination that has been tested
together. The umbrella repository certifies known-good sets: its
[`versions.yaml`](https://github.com/jcsvwinston/quantum/blob/main/versions.yaml)
manifest records, per suite release, the trio of module versions that were
validated as a unit:

```text
# what the manifest certifies (excerpt shape, not current values)
quantum: "X.Y.Z"        # suite version — its own line, not a module version
modules:
  quark:   "vA.B.C"
  nucleus: "vD.E.F"
  orbit:   "vG.H.I"
```

How to use it when you depend on more than one module:

- **Prefer a certified trio.** Take the module versions from the most
  recent certified `versions.yaml` entry rather than mixing each module's
  latest tag.
- **Newer patches are fine within `v1.x`** — the certification is a
  floor, not a ceiling; Go's version resolution will happily select a newer
  compatible patch when another dependency requires it.
- **Mind the module pins.** `orbit` pins the `nucleus` version it was built
  against in its own `go.mod`; a nucleus fix reaches an orbit-using app
  once your app's `go.mod` (or a newer orbit release) requires the newer
  nucleus. `go list -m all | grep jcsvwinston` shows what actually resolved.

## When a default changes under you

Minor releases never remove stable surfaces, but they may **tighten
security defaults** with an explicit opt-out. The pattern to expect, taken
from real releases:

- v1.0.0 flipped CORS to deny-by-default — apps that needed the old
  behavior set `cors_origins: ["*"]` explicitly.
- v1.2.0 started ignoring `X-Forwarded-For` unless `trusted_proxies` is
  configured, and started rejecting `jwt_secret` values shorter than
  32 bytes at boot.

If an upgrade makes your app fail at boot, that is usually this pattern
working as intended: the error message names the key, and the
[release notes](../reference/release-notes.md) name the escape hatch.

## Breaking changes and `v2`

Removals and incompatible changes to stable surfaces are reserved for a new
major version, preceded by the three-stage deprecation cycle (marked →
warned at runtime → removed at the announced release) described in
[Support & compatibility](../architecture/compatibility.md#how-deprecation-works).
If a `v2` ever ships:

- it imports as a new module path (Go semantic import versioning requires
  a `/v2` suffix), so `v1` and `v2` can coexist during a migration;
- every removal will already have shipped a deprecation warning and a
  documented replacement during `v1.x`;
- a migration guide will accompany the release — mechanical renames come
  with migration notes precise enough to apply with an editor.

There is no `v2` planned or in progress at the time of this release; the
current line is `v1.x`, and staying current on it is the supported path.

## If an upgrade breaks you

An upgrade within `v1.x` that breaks code using only stable surfaces is a
framework bug. Open an issue with the two versions and a minimal
reproduction — see
[Support & compatibility](../architecture/compatibility.md#reporting-a-compatibility-problem).
