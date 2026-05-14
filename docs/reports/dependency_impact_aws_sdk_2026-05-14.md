# Dependency Impact Review — AWS SDK for Go v2
**Date:** 2026-05-14
**Reviewer:** dependency-impact subagent
**Trigger:** ADR-005 (ES256 + AWS Secrets Manager resolver)
**Verdict:** ACCEPT-WITH-NOTE

---

## Summary

`pkg/auth/secrets` introduces the first cloud-vendor SDK into the Nucleus
tree: `github.com/aws/aws-sdk-go-v2/config` and
`github.com/aws/aws-sdk-go-v2/service/secretsmanager`. The SDK is correctly
confined behind a one-method unexported interface (`secretsManagerAPI`). No
AWS type appears in any exported `pkg/*` signature. All contract and firewall
tests pass. The addition is architecturally sound; two cosmetic notes are
recorded below.

---

## Transitive Set

All 15 entries appear in `go.mod` as `// indirect` due to the pre-existing
`admin/proto` replace-directive that prevents `go mod tidy` from running (see
NOTE 1). The actual import graph from `go list -deps` produces 80 AWS/smithy
sub-packages across the following versioned modules:

| Module | Version | Role |
|---|---|---|
| `github.com/aws/aws-sdk-go-v2` | v1.41.7 | Core SDK primitives (`aws.Config`, retry, signer, transport) |
| `github.com/aws/aws-sdk-go-v2/config` | v1.32.17 | **Direct** — default credential/config chain loader |
| `github.com/aws/aws-sdk-go-v2/credentials` | v1.19.16 | Transitive — static, process, EC2, SSO credential providers |
| `github.com/aws/aws-sdk-go-v2/feature/ec2/imds` | v1.18.23 | Transitive — EC2 Instance Metadata Service credential source |
| `github.com/aws/aws-sdk-go-v2/internal/configsources` | v1.4.23 | Transitive — endpoint config resolution |
| `github.com/aws/aws-sdk-go-v2/internal/endpoints/v2` | v2.7.23 | Transitive — endpoint rule evaluation |
| `github.com/aws/aws-sdk-go-v2/internal/v4a` | v1.4.24 | Transitive — SigV4a asymmetric request signing |
| `github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding` | v1.13.9 | Transitive — HTTP accept-encoding middleware |
| `github.com/aws/aws-sdk-go-v2/service/internal/presigned-url` | v1.13.23 | Transitive — pre-signed URL support (STS) |
| `github.com/aws/aws-sdk-go-v2/service/secretsmanager` | v1.41.7 | **Direct** — `GetSecretValue` API client |
| `github.com/aws/aws-sdk-go-v2/service/signin` | v1.0.11 | Transitive — IAM Identity Center federation |
| `github.com/aws/aws-sdk-go-v2/service/sso` | v1.30.17 | Transitive — SSO credential provider |
| `github.com/aws/aws-sdk-go-v2/service/ssooidc` | v1.35.21 | Transitive — SSO OIDC token exchange |
| `github.com/aws/aws-sdk-go-v2/service/sts` | v1.42.1 | Transitive — AssumeRole credential provider |
| `github.com/aws/smithy-go` | v1.25.1 | Transitive — AWS protocol/middleware runtime shared by all v2 SDKs |

**Direct imports in source:** `config` and `secretsmanager` only (confirmed
by `grep -r aws-sdk-go-v2 pkg/ --include="*.go" -l`; result: one file,
`pkg/auth/secrets/aws.go`).

**go.mod cosmetic discrepancy (NOTE 1):** both `config` and `secretsmanager`
appear marked `// indirect` in `go.mod` even though `pkg/auth/secrets` imports
them directly. This is a known side-effect of the `admin/proto` replace
directive that prevents `go mod tidy` from running cleanly. It is a
presentation artefact only — the modules are correctly pinned at the right
versions. Resolve by running `go mod tidy` once the replace-directive issue is
unblocked.

---

## Blast Radius / Firewall

**Import confinement:** `grep -r aws-sdk-go-v2 . --include="*.go" -l` returns
exactly one file: `pkg/auth/secrets/aws.go`. No other package in the tree
imports the AWS SDK.

**Exported signature check:** `go vet ./pkg/...` passes clean. No
`secretsmanager.*`, `aws.*`, `config.*`, or `smithy.*` type appears in any
exported function, struct, or interface in `pkg/*`. The SDK is hidden behind
the unexported `secretsManagerAPI` interface; `NewAWSSecretsManagerResolver`
returns `secrets.Resolver` (a framework type).

**Contract freeze test:** `go test ./contracts/... -run TestFirewall` —
PASS. `go test ./contracts/...` (all freeze tests) — PASS.

**Lazy linkage:** `App.New` only constructs the AWS client when at least one
`jwt_keys[]` entry carries an `aws-sm:` prefix (`secrets.HasManagedScheme`
gate). Deployments with no `aws-sm:` references never touch the credential
chain; the SDK packages still link into the binary but no I/O or credential
resolution occurs at startup.

---

## License

| Module | License |
|---|---|
| `github.com/aws/aws-sdk-go-v2` (all sub-modules) | Apache-2.0 |
| `github.com/aws/smithy-go` (all sub-modules) | Apache-2.0 |

Apache-2.0 is compatible with this project (single-maintainer, pre-1.0,
no declared outbound license constraint). No patent retaliation or
attribution conflict with existing dependencies.

---

## CVEs / Maturity

These are AWS-official, actively maintained modules. `aws-sdk-go-v2` v1.41.x
and `smithy-go` v1.25.x are current releases as of 2026-05-14. No known
advisories against these versions. The SDK is modular by design — only
`secretsmanager` and its credential-chain dependencies are pulled; unrelated
services (S3, DynamoDB, etc.) are not.

---

## Binary-Size / Build-Time Impact

The `config` module transitively pulls credential providers for SSO, STS,
EC2 IMDS, and process credentials — this is the full default credential chain,
unavoidable when using `awsconfig.LoadDefaultConfig`. Rough binary-size
estimate: +3–5 MB to linked binaries that reach `pkg/auth/secrets` (primarily
`cmd/goframe`). Build time impact is negligible; all modules are pure Go with
no CGo. Binaries that do not import `pkg/auth/secrets` are unaffected (none
in this repo currently avoid it since `pkg/app` imports `pkg/auth/secrets`
unconditionally, but the runtime cost is zero when no `aws-sm:` key is
configured).

**NOTE 2:** `pkg/app` currently imports `pkg/auth/secrets` unconditionally
(wired in `jwt_setup.go`). This means every Nucleus binary pays the link cost
of the AWS SDK even when AWS Secrets Manager is never used. A future
optimisation — moving the `AWSSecretsManagerResolver` construction behind a
build tag or a plugin — would recover this. Acceptable for pre-1.0; record as
a known trade-off for Phase 4 modularisation.

---

## Recommended Follow-ups

1. Unblock `go mod tidy` (admin/proto replace-directive) and re-run so
   `config` and `secretsmanager` are correctly marked as direct, not
   indirect, in `go.mod`.
2. Track NOTE 2 (unconditional link cost) as a Phase 4 item: consider a
   `secrets_aws` build tag or plugin interface so non-AWS deployments can
   exclude the SDK from their binary.
3. When GCP Secret Manager or Azure Key Vault resolvers are added
   (ADR-005 non-goals, deferred), repeat this review for those SDKs.
