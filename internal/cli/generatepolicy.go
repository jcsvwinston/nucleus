// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Route-table printing and `--with-policy` seeding for the generators
// (GF-04). The audit measured the first POST against a freshly generated
// resource as a 403→404→419 ladder: the default-deny authorizer masks the
// 404 of a guessed path, the generator never said which routes it mounted,
// and the scaffold's CSRF exemptions do not cover the new API. This file
// closes the three gaps: the generator prints the exact route table it
// wired, and --with-policy seeds the anonymous RBAC rows plus the CSRF
// exemption the way `generate module` already carries them.
package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// printMountedRouteTable prints the concrete routes the generated module
// registers, so the developer authorizes real paths instead of guessed
// ones. pageRoute is empty when the scaffold has no HTML page (generate
// resource); resourcePath is the REST collection path ("/books").
func printMountedRouteTable(w io.Writer, pageRoute, resourcePath string) {
	fmt.Fprintln(w, "Routes the module mounts:")
	if pageRoute != "" {
		fmt.Fprintf(w, "  GET     %-24s page (HTML)\n", pageRoute)
	}
	fmt.Fprintf(w, "  GET     %-24s index\n", resourcePath)
	fmt.Fprintf(w, "  POST    %-24s create\n", resourcePath)
	fmt.Fprintf(w, "  GET     %-24s show\n", resourcePath+"/{id}")
	fmt.Fprintf(w, "  PUT     %-24s update\n", resourcePath+"/{id}")
	fmt.Fprintf(w, "  DELETE  %-24s destroy\n", resourcePath+"/{id}")
}

// resourcePolicyRows returns the anonymous CRUD rows for a resource path,
// in the same shape the scaffold's rbac_policy.csv documents (framework
// CRUD verbs, not raw HTTP methods).
func resourcePolicyRows(resourcePath string) []string {
	return []string{
		fmt.Sprintf("p, anonymous, %s, read, allow", resourcePath),
		fmt.Sprintf("p, anonymous, %s, create, allow", resourcePath),
		fmt.Sprintf("p, anonymous, %s/*, read, allow", resourcePath),
		fmt.Sprintf("p, anonymous, %s/*, update, allow", resourcePath),
		fmt.Sprintf("p, anonymous, %s/*, delete, allow", resourcePath),
	}
}

// seedResourcePolicy implements --with-policy: it appends the anonymous
// CRUD rows for resourcePath to the project's RBAC policy CSV and adds the
// resource path to csrf_exempt_paths in the project config, reporting every
// edit on stdout. Rows already present are not duplicated. Development
// convenience only — the rows grant anonymous access and should be scoped
// down before production.
func seedResourcePolicy(outDir, configPath, resourcePath string, stdout io.Writer) error {
	if configPath == "" {
		candidate := filepath.Join(outDir, "nucleus.yml")
		if _, err := os.Stat(candidate); err == nil {
			configPath = candidate
		}
	}

	policyFile := "rbac_policy.csv"
	if configPath != "" {
		if cfg, err := loadConfig(configPath); err == nil && strings.TrimSpace(cfg.RBACPolicyFile) != "" {
			policyFile = strings.TrimSpace(cfg.RBACPolicyFile)
		}
	}
	if !filepath.IsAbs(policyFile) {
		policyFile = filepath.Join(outDir, policyFile)
	}

	if err := appendPolicyRows(policyFile, resourcePath, stdout); err != nil {
		return err
	}

	if configPath == "" {
		fmt.Fprintf(stdout, "  csrf: no config file found under %s — add %q to csrf_exempt_paths yourself\n", outDir, resourcePath)
		return nil
	}
	return addCSRFExemption(configPath, resourcePath, stdout)
}

func appendPolicyRows(policyFile, resourcePath string, stdout io.Writer) error {
	existing := ""
	if raw, err := os.ReadFile(policyFile); err == nil {
		existing = string(raw)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read policy file %s: %w", policyFile, err)
	}

	missing := make([]string, 0, 5)
	for _, row := range resourcePolicyRows(resourcePath) {
		if !strings.Contains(existing, row) {
			missing = append(missing, row)
		}
	}
	if len(missing) == 0 {
		fmt.Fprintf(stdout, "  policy: rows for %s already present in %s\n", resourcePath, policyFile)
		return nil
	}

	var b strings.Builder
	b.WriteString(existing)
	if existing != "" && !strings.HasSuffix(existing, "\n") {
		b.WriteString("\n")
	}
	b.WriteString(fmt.Sprintf("\n# nucleus generate resource --with-policy: anonymous CRUD on %s.\n", resourcePath))
	b.WriteString("# Development defaults — scope these down when you add authentication.\n")
	for _, row := range missing {
		b.WriteString(row)
		b.WriteString("\n")
	}
	if err := os.WriteFile(policyFile, []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("write policy file %s: %w", policyFile, err)
	}
	fmt.Fprintf(stdout, "  policy: %d row(s) for %s appended to %s\n", len(missing), resourcePath, policyFile)
	return nil
}

// addCSRFExemption adds resourcePath to csrf_exempt_paths in the YAML
// config file. It edits the two shapes the scaffold and the docs show —
// an inline flow list (`csrf_exempt_paths: ["/api/"]`) and a missing key —
// and refuses to guess on anything else (block-style lists, commented-out
// keys), printing the manual edit instead of corrupting the file.
func addCSRFExemption(configFile, resourcePath string, stdout io.Writer) error {
	raw, err := os.ReadFile(configFile)
	if err != nil {
		return fmt.Errorf("read config file %s: %w", configFile, err)
	}

	lines := strings.Split(string(raw), "\n")
	quoted := fmt.Sprintf("%q", resourcePath)
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "csrf_exempt_paths:") {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "csrf_exempt_paths:"))
		if strings.Contains(rest, quoted) || strings.Contains(rest, "'"+resourcePath+"'") {
			fmt.Fprintf(stdout, "  csrf: %s already exempt in %s\n", resourcePath, configFile)
			return nil
		}
		if strings.HasPrefix(rest, "[") && strings.HasSuffix(rest, "]") {
			inner := strings.TrimSpace(rest[1 : len(rest)-1])
			if inner == "" {
				lines[i] = strings.Replace(line, rest, "["+quoted+"]", 1)
			} else {
				lines[i] = strings.Replace(line, rest, "["+inner+", "+quoted+"]", 1)
			}
			if err := os.WriteFile(configFile, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
				return fmt.Errorf("write config file %s: %w", configFile, err)
			}
			fmt.Fprintf(stdout, "  csrf: %s added to csrf_exempt_paths in %s\n", resourcePath, configFile)
			return nil
		}
		// Block-style or otherwise unexpected shape: do not guess.
		fmt.Fprintf(stdout, "  csrf: csrf_exempt_paths in %s is not an inline list — add %s to it yourself\n", configFile, quoted)
		return nil
	}

	// Key absent: append it. With csrf_enabled unset/false the key is
	// inert; with the MVC scaffold (csrf_enabled: true) it is required for
	// cookie-less JSON POST/PUT/DELETE.
	out := string(raw)
	if out != "" && !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	out += fmt.Sprintf("# Added by nucleus generate resource --with-policy: the JSON API takes\n# cookie-less POST/PUT/DELETE and cannot present a CSRF token.\ncsrf_exempt_paths: [%s]\n", quoted)
	if err := os.WriteFile(configFile, []byte(out), 0o644); err != nil {
		return fmt.Errorf("write config file %s: %w", configFile, err)
	}
	fmt.Fprintf(stdout, "  csrf: csrf_exempt_paths [%s] added to %s\n", quoted, configFile)
	return nil
}
