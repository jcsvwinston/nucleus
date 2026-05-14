# Compatibility Report

- Generated at (UTC): 2026-05-14T10:23:00Z
- Branch: `claude/interesting-ishizaka-d51a45`
- Commit: `fce7c57`

## Fixture Applications

- Harness status: success


- Generated at (UTC): 2026-05-14T10:23:00Z
- Branch: `claude/interesting-ishizaka-d51a45`
- Commit: `fce7c57`
- Profiles analyzed: 3

| Profile | Status | Duration | Command |
| --- | --- | --- | --- |
| minimal-api | success | 3s | `go test ./examples/mvc_api/cmd/server -run '^TestExampleMVCAPI_Minimal_Smoke$' -count=1 -v` |
| admin-heavy | success | 2s | `go test ./examples/mvc_api/cmd/server -run '^TestExampleMVCAPIAdmin_Smoke$' -count=1 -v` |
| plugin-heavy | success | 0s | `go test ./examples/plugins/... -count=1 -v` |

## Summary

- Passed profiles: 3/3 (100%)
- Threshold: >= 100%
- Decision: READY

## Stable Contract Summary

| Contract Scope | Status | Duration | Command |
| --- | --- | --- | --- |
| stable-api-app-core | success | 7s | `go test ./pkg/app -run '^Test(AppNew_|AppRegisterModel|AppShutdown_|AppMethods_)' -count=1` |
| stable-api-http-data | success | 2s | `go test ./pkg/router ./pkg/model ./pkg/db -count=1` |
| stable-cli | success | 2s | `go test ./internal/cli -count=1` |
| stable-plugin-sdk | success | 1s | `go test ./pkg/plugins ./examples/plugins/... -count=1` |
| stable-config | success | 2s | `go test ./pkg/app -run '^TestLoadConfig_|^TestConfig_' -count=1` |
| stable-contract-freeze | success | 4s | `bash scripts/ci/check_contract_freeze.sh` |
| firewall-type-leaks | success | 3s | `go test ./contracts -run '^TestFirewall_' -count=1` |
| stable-contract-docs | success | 0s | `test -f docs/reference/API_CONTRACT_INVENTORY.md && test -f docs/reference/CLI_CONTRACT_MATRIX.md && test -f docs/reference/CONFIG_KEY_REGISTRY.md && test -f docs/governance/DEPRECATION_TEMPLATE.md && test -f docs/governance/MIGRATION_ASSISTANT_CONVENTIONS.md && test -f docs/templates/deprecation_notice.md && test -f docs/templates/migration_assistant.md` |

- Stable contract checks passed: 8/8 (100%)
- Compatibility statement: no breaking changes detected in validated stable contracts
- Decision: READY
