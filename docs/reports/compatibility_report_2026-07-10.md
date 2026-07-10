# Compatibility Report

- Generated at (UTC): 2026-07-10T17:58:43Z
- Branch: `chore/v1-gate-waivers-and-rehearsal`
- Commit: `a69baed8`

## Fixture Applications

- Harness status: success


- Generated at (UTC): 2026-07-10T17:58:43Z
- Branch: `chore/v1-gate-waivers-and-rehearsal`
- Commit: `a69baed8`
- Profiles analyzed: 3

| Profile | Status | Duration | Command |
| --- | --- | --- | --- |
| core-build | success | 11s | `GOWORK=off go build ./pkg/... ./cmd/nucleus ./internal/cli/...` |
| mvc-api | success | 10s | `GOWORK=off go build ./examples/mvc_api/... && GOWORK=off go test ./examples/mvc_api/...` |
| showcase-suite | success | 50s | `cd '/Users/jcsv/GolandProjects/quantum/nucleus/examples/showcase_demo' && GOWORK='/var/folders/9r/mvbn7z5x79l0h5jjbq28ggd80000gn/T/tmp.hAkjw8fcXW/showcase.go.work' go build ./...` |

## Summary

- Passed profiles: 3/3 (100%)
- Threshold: >= 100%
- Decision: READY

## Stable Contract Summary

| Contract Scope | Status | Duration | Command |
| --- | --- | --- | --- |
| stable-api-app-core | success | 3s | `go test ./pkg/app -run '^Test(AppNew_|AppRegisterModel|AppShutdown_|AppMethods_)' -count=1` |
| stable-api-http-data | success | 2s | `go test ./pkg/router ./pkg/model ./pkg/db -count=1` |
| stable-cli | success | 21s | `go test ./internal/cli -count=1` |
| stable-plugin-sdk | success | 0s | `go test ./pkg/plugins -count=1` |
| stable-config | success | 2s | `go test ./pkg/app -run '^TestLoadConfig_|^TestConfig_' -count=1` |
| stable-contract-freeze | success | 6s | `bash scripts/ci/check_contract_freeze.sh` |
| firewall-type-leaks | success | 3s | `go test ./contracts -run '^TestFirewall_' -count=1` |
| stable-contract-docs | success | 0s | `test -f docs/reference/API_CONTRACT_INVENTORY.md && test -f docs/reference/CLI_CONTRACT_MATRIX.md && test -f docs/reference/CONFIG_KEY_REGISTRY.md && test -f docs/governance/DEPRECATION_TEMPLATE.md && test -f docs/governance/MIGRATION_ASSISTANT_CONVENTIONS.md && test -f docs/templates/deprecation_notice.md && test -f docs/templates/migration_assistant.md` |

- Stable contract checks passed: 8/8 (100%)
- Compatibility statement: no breaking changes detected in validated stable contracts
- Decision: READY
