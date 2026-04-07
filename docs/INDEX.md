# Documentation Map

Reference date: 2026-04-07.
Status: Current.

This file is the canonical entrypoint for GoFrame documentation.

## Start Here

- [QUICKSTART.md](QUICKSTART.md)
- [DEVELOPER_MANUAL.md](DEVELOPER_MANUAL.md)
- [PROJECT_LAYOUT.md](PROJECT_LAYOUT.md)
- [../SPEC.md](../SPEC.md)

## Core Engineering References

- [MODELING_MULTI_DATABASE.md](MODELING_MULTI_DATABASE.md)
- [CLI_BEST_PRACTICES.md](CLI_BEST_PRACTICES.md)
- [API_CONTRACT_INVENTORY.md](API_CONTRACT_INVENTORY.md)
- [CLI_CONTRACT_MATRIX.md](CLI_CONTRACT_MATRIX.md)
- [CONFIG_KEY_REGISTRY.md](CONFIG_KEY_REGISTRY.md)
- [PLUGIN_SDK.md](PLUGIN_SDK.md)
- [PLUGIN_EXAMPLES.md](PLUGIN_EXAMPLES.md)
- [MAIL_PROVIDERS.md](MAIL_PROVIDERS.md)
- [OBSERVABILITY_BASELINE.md](OBSERVABILITY_BASELINE.md)

## Strategy and Governance

- [ENTERPRISE_LONG_TERM_ROADMAP.md](ENTERPRISE_LONG_TERM_ROADMAP.md)
- [ROADMAP_SUPERAR_DJANGO.md](ROADMAP_SUPERAR_DJANGO.md)
- [COMPATIBILITY_SLO.md](COMPATIBILITY_SLO.md)
- [VERSIONING.md](VERSIONING.md)
- [DEPRECATION_TEMPLATE.md](DEPRECATION_TEMPLATE.md)
- [MIGRATION_ASSISTANT_CONVENTIONS.md](MIGRATION_ASSISTANT_CONVENTIONS.md)
- [GO_VERSION_POLICY.md](GO_VERSION_POLICY.md)
- [CI_MATRIX.md](CI_MATRIX.md)
- [RELEASE_CHECKLIST.md](RELEASE_CHECKLIST.md)
- [../CHANGELOG.md](../CHANGELOG.md)

## Validation Reports

- [reports/exploratory_stability.md](reports/exploratory_stability.md)
- [reports/exploratory_stability_postfix.md](reports/exploratory_stability_postfix.md)
- [reports/exploratory_stability_postfix_10runs.md](reports/exploratory_stability_postfix_10runs.md)
- [reports/compatibility_harness_latest.md](reports/compatibility_harness_latest.md)
- [reports/dependency_critical_review_2026-04-07.md](reports/dependency_critical_review_2026-04-07.md)
- [reports/release_readiness_2026-04-07.md](reports/release_readiness_2026-04-07.md)

## Precedence Rule

When documents conflict, use this precedence:

1. `README.md`
2. strategy/governance docs listed above
3. detailed implementation docs
4. historical behavior only from git history (not separate phase files)

## Terminology

- External provider binaries: `goframe-plugin-<provider>`
- Legacy mail fallback naming: `goframe-mail-<driver>`
