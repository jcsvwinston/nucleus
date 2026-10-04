// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleus

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ErrModuleDependency reports a DependsOn declaration the application cannot
// honour: a name that is not mounted, or a cycle. The message names the
// modules involved. Boot stops before any database pool or module is
// started.
var ErrModuleDependency = errors.New("nucleus: invalid module dependency")

// moduleDependencyCarrier is the unexported view the framework type-asserts
// on a ModuleSpec to read its DependsOn declaration. Only the framework's own
// moduleSpec[C] implements it; a foreign ModuleSpec declares nothing and
// starts in name order. Kept off the public ModuleSpec contract — adding a
// method to that interface would break every implementation outside this
// package.
type moduleDependencyCarrier interface {
	dependsOn() []string
}

// declaredDependencies returns the distinct module names spec declares in
// DependsOn, blank entries dropped, in declaration order.
func declaredDependencies(spec ModuleSpec) []string {
	c, ok := spec.(moduleDependencyCarrier)
	if !ok {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, d := range c.dependsOn() {
		d = strings.TrimSpace(d)
		if d == "" || seen[d] {
			continue
		}
		seen[d] = true
		out = append(out, d)
	}
	return out
}

// moduleStartOrder returns the keys of modules in the order the application
// starts them: a topological order of the DependsOn declarations, ties broken
// by key. With no declarations it is exactly sortedModuleSpecs' order — the
// order every application had before DependsOn existed. Shutdown runs it in
// reverse.
//
// Dependencies name modules by Name(); the map key is the identity used for
// the order (the builder keys App.Modules by Name, so the two agree; a
// direct-struct App that keys differently is still ordered by its keys).
func moduleStartOrder(modules map[string]ModuleSpec) ([]string, error) {
	if len(modules) == 0 {
		return nil, nil
	}
	keys := make([]string, 0, len(modules))
	byName := make(map[string]string, len(modules)) // Name() -> key
	for k, spec := range modules {
		keys = append(keys, k)
		byName[spec.Name()] = k
	}
	sort.Strings(keys)

	// deps[k] = keys k depends on; dependents[k] = keys that depend on k.
	deps := make(map[string][]string, len(keys))
	dependents := make(map[string][]string, len(keys))
	for _, k := range keys {
		for _, d := range declaredDependencies(modules[k]) {
			target, ok := byName[d]
			if !ok {
				mounted := make([]string, 0, len(byName))
				for n := range byName {
					mounted = append(mounted, n)
				}
				sort.Strings(mounted)
				return nil, fmt.Errorf("%w: module %q depends on %q, which is not mounted (mounted: %s)",
					ErrModuleDependency, modules[k].Name(), d, strings.Join(mounted, ", "))
			}
			deps[k] = append(deps[k], target)
			dependents[target] = append(dependents[target], k)
		}
	}

	// Kahn's algorithm with the ready set kept sorted, so among the modules
	// whose dependencies have all started the next one is always the
	// smallest key — the tie-break that keeps today's order for modules that
	// declare nothing.
	pending := make(map[string]int, len(keys))
	var ready []string
	for _, k := range keys {
		pending[k] = len(deps[k])
		if pending[k] == 0 {
			ready = append(ready, k)
		}
	}
	order := make([]string, 0, len(keys))
	for len(ready) > 0 {
		next := ready[0]
		ready = ready[1:]
		order = append(order, next)
		for _, d := range dependents[next] {
			pending[d]--
			if pending[d] == 0 {
				ready = append(ready, d)
				sort.Strings(ready)
			}
		}
	}
	if len(order) == len(keys) {
		return order, nil
	}

	// What is left is on a cycle or behind one. Walk the dependency edges
	// from the smallest remaining key until a key repeats: that stretch is a
	// cycle, and naming it is what lets the author fix it.
	left := map[string]bool{}
	for _, k := range keys {
		if pending[k] > 0 {
			left[k] = true
		}
	}
	return nil, fmt.Errorf("%w: module dependency cycle: %s", ErrModuleDependency, describeCycle(modules, deps, left))
}

// describeCycle returns one cycle among the left keys as "a -> b -> a",
// naming modules by Name(). Deterministic: it starts from the smallest key
// and follows each module's smallest remaining dependency.
func describeCycle(modules map[string]ModuleSpec, deps map[string][]string, left map[string]bool) string {
	start := ""
	for k := range left {
		if start == "" || k < start {
			start = k
		}
	}
	pos := map[string]int{}
	var path []string
	cur := start
	for {
		if i, seen := pos[cur]; seen {
			cycle := append(path[i:], cur)
			names := make([]string, len(cycle))
			for j, k := range cycle {
				names[j] = fmt.Sprintf("%q", modules[k].Name())
			}
			return strings.Join(names, " -> ")
		}
		pos[cur] = len(path)
		path = append(path, cur)
		next := ""
		for _, d := range deps[cur] {
			if left[d] && (next == "" || d < next) {
				next = d
			}
		}
		if next == "" {
			// Unreachable: every key left by Kahn's algorithm still waits
			// on another key that is left.
			return fmt.Sprintf("%q", modules[cur].Name())
		}
		cur = next
	}
}

// specsInOrder returns the specs of modules in the order of keys.
func specsInOrder(modules map[string]ModuleSpec, keys []string) []ModuleSpec {
	out := make([]ModuleSpec, 0, len(keys))
	for _, k := range keys {
		out = append(out, modules[k])
	}
	return out
}

// moduleDependencyNames returns each module's declared DependsOn, keyed by
// Name(), for the provide/resolve registry's undeclared-dependency warning.
func moduleDependencyNames(specs []ModuleSpec) map[string][]string {
	out := make(map[string][]string, len(specs))
	for _, spec := range specs {
		if deps := declaredDependencies(spec); len(deps) > 0 {
			out[spec.Name()] = deps
		}
	}
	return out
}
