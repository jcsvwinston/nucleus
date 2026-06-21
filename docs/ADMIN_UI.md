# Admin UI

Reference date: 2026-06-21.
Status: Relocated — this feature no longer ships in the Nucleus core.

## Moved to orbit

The admin UI (React SPA, live inspector, Data Studio, RBAC panel) was extracted
from the Nucleus core into **orbit**, a separate pluggable Go module, per
[ADR-019](adrs/ADR-019-extract-admin-to-orbit-module.md).

The in-core `pkg/admin` package was removed in the clean break of 2026-06-21
(PR #155 removed the panel; PR #159 removed the observability subsystem).

For documentation, installation, and configuration of the admin UI, refer to
the orbit repository: **github.com/jcsvwinston/orbit**.

Orbit mounts through the standard Nucleus extension surface (ADR-010):

```go
app.Mount(orbit.Module(orbit.Config{Prefix: "/admin"}))
```
