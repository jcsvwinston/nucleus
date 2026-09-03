## What changes and why

## Checklist

- [ ] `make check` passes locally (vet, guards, tests).
- [ ] Behaviour changes have tests; docs changed in the same PR when a command, key or API moved.
- [ ] If a frozen surface changed on purpose (exported symbol, CLI command, config key, extension field, security default): `make regen-baselines` ran and the regenerated files are in this PR, with the reason below.
- [ ] The PR title is the conventional commit release-please will read (squash merge).
