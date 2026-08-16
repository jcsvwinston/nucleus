# pkg/validate — contract

Lifecycle: `stable`. Split out of `API_CONTRACT_INVENTORY.md` (DX-12):
the machine-auditable contract prose lives here, one file per package, so
the inventory table stays readable for humans.

## Contract scope

Validation entrypoint + custom rule registration

## Notes

Shared validation boundary for handlers/models. **Firewall allow-list (ADR-015 §3d):** `RegisterRule(tag string, fn validator.Func, message string)` deliberately exposes `validator.Func` from `github.com/go-playground/validator/v10` — it is the documented custom-rule extension point; `validator.Func`'s parameter (`validator.FieldLevel`) is a fat interface that cannot be meaningfully re-wrapped under a Nucleus type. Tracked in `contracts/firewall_test.go` `blessedLeaks`.
