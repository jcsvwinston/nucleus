// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

package knownproviders

// LinkedByImports returns the catalog entries a project links, given the
// import paths of its Go files: the entries whose ImportPath — the blank
// import `nucleus add` writes — is among them, in catalog order. A core
// entry (Ships == InCore) registers through the framework module itself and
// is never "not linked", so only the entries shipped as modules count.
//
// It exists for the commands that validate a configuration from inside the
// `nucleus` binary (doctor, health, config print). The binary links none of
// providers/*, so its registries do not know `s3`, `ldap` or `sentry`, and
// without this the block `nucleus add sentry` had just written was refused
// as an unknown key by the next command (NU-132). The project's imports
// are what the running application registers, so they are what the CLI
// should accept. Reading the files is the CLI's job (it already parses
// main.go to write the import); this package stays free of go/parser so a
// running application, which links the catalog through its configuration
// validators, does not link the Go parser for nothing.
func LinkedByImports(imported map[string]bool) []Entry {
	var out []Entry
	for _, e := range catalog {
		if e.Ships != AsModule {
			continue
		}
		if path := e.ImportPath(); path != "" && imported[path] {
			out = append(out, e)
		}
	}
	return out
}
