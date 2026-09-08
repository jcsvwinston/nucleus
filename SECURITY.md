# Security Policy

## Supported Versions

The current release is **v1.25.0** <!-- x-release-please-version --> on the
stable `v1.x` line (release-please rewrites that version on every release,
and `scripts/ci/check_version_claims.sh` fails CI if it drifts).

Security fixes land on:

- `main` (always), and
- the **latest two tagged minor lines**, via patch releases when a fix
  warrants one.

Older tags are not patched — upgrade to a current tag to receive security
updates. Within `v1.x` that upgrade is bounded by the
[Compatibility SLO](docs/governance/COMPATIBILITY_SLO.md): stable surfaces
do not break in a minor.

## Verifying a Release

Releases cut from the first signed tag onward publish six CLI archives, one
SPDX SBOM per archive, a `checksums.txt` covering all of them, a keyless
cosign signature over that checksum file (`checksums.txt.sig` and
`checksums.txt.pem`), and a build provenance attestation.

Earlier releases publish at most the archives and the checksum file, and
nothing that proves where they came from. Many publish no assets at all:
every release from v1.0.0 to v1.15.1, and several as late as v1.22.0, have
an empty asset list. Signing cannot be applied retroactively: a release is
signed by the run that builds it, with a certificate minted for that run and
valid for minutes. The release page is what tells you which kind you are
looking at — a signed release lists `checksums.txt.sig`, one that lists
archives and a `checksums.txt` can be checked for integrity but not for
origin, and one that lists no assets offers neither.

There is no long-lived signing key, and none is published: what you verify is
which workflow, in which repository, at which tag produced the release.

The release workflow is dispatched at the **tag** ref, so the certificate
identity ends in `.../release.yml@refs/tags/vX.Y.Z` — not `@refs/heads/main`,
which is what every cosign example shows and what verifies nothing here.

The two commands, with the exact identity string and the failure modes, are
on the public site's
[Operations → Verifying a release](https://jcsvwinston.github.io/quantum/nucleus/operations/verifying-releases)
page (source: [`website/docs/operations/verifying-releases.md`](website/docs/operations/verifying-releases.md)).

## The build's own supply chain

Every `uses:` in `.github/workflows/` names a 40-hex commit SHA, with the
release tag it came from in a trailing comment. A tag is a moving pointer:
whoever can move `v7` can change what runs with this repository's token,
and a compromised action reaches the release job. A SHA cannot be moved.

A pin that nobody updates is worse than a tag, because it freezes the fixes
too. `.github/dependabot.yml` lists the `github-actions` ecosystem for that
reason: Dependabot reads the tag from the comment, and a new release of an
action arrives as a pull request that rewrites the SHA and the comment
together. Update a pin through that pull request, not by hand.

`.github/workflows/scorecard.yml` runs OpenSSF Scorecard weekly and on
demand. It reports and does not gate: the findings are uploaded as
code-scanning alerts, and no pull request fails because of them. Publishing
the score to the public OpenSSF dataset is off (`publish_results: false`);
turning it on is the repository owner's decision, not the workflow's.

Two of its checks cannot be answered from inside the workflow.
Branch-Protection needs an admin-scoped read of the branch settings, and
`administration` is not a scope a `permissions:` block can grant the default
token; the workflow therefore passes
`repo_token: ${{ secrets.SCORECARD_TOKEN || github.token }}`, so adding a
fine-grained token with `Administration: Read-only` as the `SCORECARD_TOKEN`
secret is all it takes to make that check report — and until someone does,
the run falls back to the default token and simply leaves it unread.
Signed-Releases reads the most recent releases that carry assets, so it
turns green on the first release cut after the signing chain above landed,
not on any change to a file.

## Static analysis of this code

`.github/workflows/codeql.yml` runs CodeQL over the framework module's Go on
every pull request, on every merge to `main` and once a week. `go vet`
already ran in CI, but it reads one function at a time; CodeQL follows a
value from where it enters the program to where it is used, which is the only
way to see a request parameter reaching a query, a path or an exec argument
several calls away.

What it covers is exactly what `go build ./...` compiles at the root: `pkg/`,
`internal/`, `cmd/` and `scripts/` — 228 of the 255 non-test `.go` files in
the tree, on a Linux runner. `contracts/` is not covered, and saying so
matters here: every one of its files is a `_test.go`, `go build` does not
compile tests, and the Go extractor ships `extract_tests: false`, so nothing
in that directory is extracted. The twelve optional modules — the drivers,
exporters and providers — and the two examples are not analysed today
either. For a compiled language CodeQL extracts what the build compiles, so
the build command is the scope; a CodeQL `paths-ignore` would do nothing
here, and there is none.

The merge trigger is part of the coverage, not housekeeping: alerts on a
pull request are reported as new relative to the most recent analysis of the
base branch, so re-analysing `main` on every merge is what keeps that
comparison honest. The weekly run catches what no merge can — a query pack
update raising an alert on code nobody has touched.

The lane reports and does not gate. Alerts appear in the pull request's
"Files changed" tab and in the Security tab, and no check fails because of
one; making a query block a merge is a repository setting, not a change to
the workflow.

## Reporting a Vulnerability

Please do not open public issues for potential vulnerabilities.

Instead:

1. Open a private GitHub Security Advisory
   ([Security → Report a vulnerability](https://github.com/jcsvwinston/nucleus/security/advisories/new))
   if available for this repository.
2. If unavailable, contact project maintainers privately and include:
   - vulnerability type and impact
   - affected version/commit
   - reproduction details or proof of concept
   - suggested mitigation (if known)

We aim to acknowledge reports quickly and provide status updates as triage progresses.

## Coordinated Disclosure

We follow coordinated disclosure whenever possible:

- report received and validated
- fix prepared and reviewed
- release published
- advisory disclosed with remediation details

## What the framework itself guarantees

The default security posture (session cookie flags, CSRF, CORS denial,
default-deny RBAC, security headers) is **measured, not transcribed**: it
is frozen in `contracts/baseline/security_posture.txt` from a real HTTP
response of a booted application, and a change in either direction fails
CI until it is stated explicitly. If you find a way to make a deployment
weaker than that baseline claims without an explicit opt-out, that is a
vulnerability — report it.

## Hardening Guidance

For production deployments, review:

- `nucleus health --deploy` and `nucleus doctor --check security`
- [`docs/reference/DEVELOPER_MANUAL.md`](docs/reference/DEVELOPER_MANUAL.md)
- [`docs/governance/RELEASE_CHECKLIST.md`](docs/governance/RELEASE_CHECKLIST.md)
- the public site's [Operations → Security](https://jcsvwinston.github.io/quantum/nucleus/operations/security) page
