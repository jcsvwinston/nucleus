# pkg/authz — contract

Lifecycle: `stable`. Split out of `API_CONTRACT_INVENTORY.md` (DX-12):
the machine-auditable contract prose lives here, one file per package, so
the inventory table stays readable for humans.

## Contract scope

`Enforcer` (creation + Casbin-backed enforcement), `AddPolicy` (allow), `Deny` (explicit deny override), `RemovePolicy` (drops both effects), authz middleware helpers; `GetPolicy() ([][]string, error)`, `GetGroupingPolicy() ([][]string, error)`, `GetAllRoles() ([]string, error)` (Casbin-free policy inspection forwarders); SSR denial-handler surface: `Denial` (fields: `Status int`, `Authenticated bool`, `Reason string`), `DenialHandler` (`func(http.ResponseWriter, *http.Request, Denial)`), `AuthzOptions` (fields: `OnDeny DenialHandler`, `ResolveSubject SubjectResolver`, `ResolveAction ActionResolver`), `(*Enforcer).MiddlewareWithOptions(opts AuthzOptions)`, `(*Enforcer).RequireRoleWithOptions(opts AuthzOptions, roles ...string)`; resolver types: `SubjectResolver` (`func(*http.Request, *auth.Claims) string`), `ActionResolver` (`func(*http.Request) string`)

## Notes

Default-deny with deny-override semantics. `Enforcer` holds the underlying Casbin enforcer in an unexported field (ADR-015 §2); Casbin's concrete type does not appear on the public surface. The three `Get*` methods are the only read-forwarding surface intentionally re-exposed after the F-4 wrap. `Middleware` and `RequireRole` delegate to their `WithOptions` counterparts with a zero `AuthzOptions{}`, so existing callers are unaffected by the additive SSR surface. `ResolveSubject` (default: `claims.UserID`) lets a role-keyed policy table use `claims.Role` as the policy subject; `ResolveAction` (default: HTTP-method mapping GET→read/POST→create/PUT
