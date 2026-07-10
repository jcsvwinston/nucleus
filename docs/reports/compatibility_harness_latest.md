# Compatibility Harness Report

- Generated at (UTC): 2026-07-10T17:58:26Z
- Branch: `chore/v1-gate-waivers-and-rehearsal`
- Commit: `a69baed8`
- Profiles analyzed: 3

| Profile | Status | Duration | Command |
| --- | --- | --- | --- |
| core-build | success | 5s | `GOWORK=off go build ./pkg/... ./cmd/nucleus ./internal/cli/...` |
| mvc-api | success | 7s | `GOWORK=off go build ./examples/mvc_api/... && GOWORK=off go test ./examples/mvc_api/...` |
| showcase-suite | success | 5s | `cd '/Users/jcsv/GolandProjects/quantum/nucleus/examples/showcase_demo' && GOWORK='/var/folders/9r/mvbn7z5x79l0h5jjbq28ggd80000gn/T/tmp.nJnLT4t322/showcase.go.work' go build ./...` |

## Summary

- Passed profiles: 3/3 (100%)
- Threshold: >= 100%
- Decision: READY
