// Package ui exposes the admin observability web UI as an embedded
// filesystem so the admin server binary is fully self-contained.
//
// Build pipeline:
//
//   1. `make ui-build` runs vite in admin/ui/ and writes admin/ui/dist/.
//   2. A tiny copy step (CI: scripts/build_admin_server_ui.sh; locally:
//      `make server-build` invokes the same logic) copies admin/ui/dist/
//      into admin/server/ui/dist/ so the //go:embed below picks it up.
//   3. `go build ./admin/server/cmd/admin-server` produces a binary that
//      serves the static UI at "/" without depending on anything on disk.
//
// During development you run `make ui-dev` (Vite on :5173 with proxy to
// :8080) and access the admin UI directly without going through the Go
// embed path. The embed path is for production binaries.
//
// The dist directory is gitignored. If a build runs without the dist in
// place, FS still embeds an empty filesystem and the server falls back
// to the placeholder index from PlaceholderHTML.
package ui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// FS returns the dist filesystem rooted at "/", or nil if the embed has
// no real UI assets (no index.html). A bare .gitkeep does NOT count as
// "the UI is built"; the explicit check on index.html is what tells us
// the build pipeline ran successfully.
func FS() fs.FS {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return nil
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil
	}
	return sub
}

// PlaceholderHTML is what the server serves at "/" when no dist has been
// built. It tells operators what to do; it never ships in a real
// release because make server-build always runs make ui-build first.
const PlaceholderHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="UTF-8">
<title>Nucleus Admin · Observability (placeholder)</title>
<meta name="viewport" content="width=device-width, initial-scale=1">
<style>
  html,body{background:#0a0a0a;color:#e4e4e7;font:14px/1.5 system-ui,sans-serif;margin:0}
  main{max-width:640px;margin:64px auto;padding:0 24px}
  code{background:#1c1c1c;padding:2px 6px;border-radius:4px}
  h1{font-size:20px;font-weight:600}
  .muted{color:#a1a1aa}
</style>
</head>
<body>
<main>
  <h1>Nucleus Admin Observability</h1>
  <p>The server is running, but no UI has been built into this binary.</p>
  <p class="muted">From the repository root:</p>
  <pre><code>make ui-build && make server-build</code></pre>
  <p class="muted">Or in development, run <code>make ui-dev</code> on :5173 with the Vite proxy.</p>
  <p class="muted">Connect-RPC endpoints under <code>/nucleus.admin.v1.*</code> are functional regardless.</p>
</main>
</body>
</html>
`
