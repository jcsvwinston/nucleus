# pkg/errors — contract

Lifecycle: `stable`. Split out of `API_CONTRACT_INVENTORY.md` (DX-12):
the machine-auditable contract prose lives here, one file per package, so
the inventory table stays readable for humans.

## Contract scope

Domain error constructors + HTTP writer

## Notes

Error payload shape and status mapping are public behavior.

Two shapes since A10 S8: `Problem`, the RFC 9457 problem details document
(`type` "about:blank", `title`, `status`, `detail`, `instance` = request
path, plus the extension members `code` and `details`), is the default of
every application since v2.0.0 (DEP-2026-022); the envelope
(`ErrorResponse`) answers under `app.WithErrorEnvelope`, and on a bare
router. `WriteError` follows the application's choice and answers problem
details whenever the client prefers `application/problem+json`;
`WriteProblem` always does; `NewProblem` builds the document. A
non-`DomainError` is a 500 whose detail never carries `err.Error()`.
