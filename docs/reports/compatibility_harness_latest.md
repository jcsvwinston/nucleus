# Compatibility Harness Report

- Generated at (UTC): 2026-05-14T10:22:53Z
- Branch: `claude/interesting-ishizaka-d51a45`
- Commit: `fce7c57`
- Profiles analyzed: 3

| Profile | Status | Duration | Command |
| --- | --- | --- | --- |
| minimal-api | success | 4s | `go test ./examples/mvc_api/cmd/server -run '^TestExampleMVCAPI_Minimal_Smoke$' -count=1 -v` |
| admin-heavy | success | 3s | `go test ./examples/mvc_api/cmd/server -run '^TestExampleMVCAPIAdmin_Smoke$' -count=1 -v` |
| plugin-heavy | success | 0s | `go test ./examples/plugins/... -count=1 -v` |

## Summary

- Passed profiles: 3/3 (100%)
- Threshold: >= 100%
- Decision: READY
