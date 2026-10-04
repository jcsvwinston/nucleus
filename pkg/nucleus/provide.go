// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleus

import (
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"sort"
	"strings"
	"sync"
)

// ErrNotProvided is returned by Resolve when no module has provided a value
// of the requested type. The message names the module that asked, the type,
// and what to do about it.
var ErrNotProvided = errors.New("nucleus: no module provides a value of this type")

// ErrAlreadyProvided is returned by Provide when a value of the same type is
// already provided. The message names both modules.
var ErrAlreadyProvided = errors.New("nucleus: a value of this type is already provided")

// Provide makes v available to every module of the application under the Go
// type T. A module calls it from its OnStart with the Runtime it received;
// another module resolves the value typed with Resolve[T].
//
// T is the type consumers ask for, so spell it when it differs from v's own
// type: a module that provides an implementation of an interface writes
// Provide[Clock](rt, systemClock{}) — Provide(rt, systemClock{}) would
// provide a systemClock, and Resolve[Clock] would not find it.
//
// One value per type: a second Provide of the same T is an error
// (ErrAlreadyProvided) naming both modules. To offer two values of one
// underlying type, give each its own named type. A nil value is refused, and
// so is a Provide after every module has started — values are provided in
// OnStart, where the modules that depend on the provider (DependsOn) can
// find them in their own OnStart.
//
// What Provide holds lives as long as the application. A value that belongs
// to one request travels under a Key instead (NewKey, SetValue, Value).
func Provide[T any](rt Runtime, v T) error {
	typ := reflect.TypeFor[T]()
	reg, module, err := servicesOf(rt, "Provide", typ)
	if err != nil {
		return err
	}
	if isNilValue(v) {
		return fmt.Errorf("nucleus: Provide[%s]: module %q provides a nil value", typ, module)
	}
	return reg.provide(typ, v, module)
}

// Resolve returns the value of type T that a module provided with Provide.
// A module calls it with the Runtime it received — typically in OnStart, and
// keeps the result for its Routes, which run after OnStart.
//
// When nothing provides T the error wraps ErrNotProvided and says what to do:
// declare the providing module in DependsOn when it has not started yet, or
// provide the value under T when a module provides it under another type
// that is assignable to T.
//
// A module that resolves a value during startup from a module it does not
// declare in DependsOn (directly or through another dependency) gets the
// value and a WARN in the log: it found it because of the order by name, and
// renaming either module would break it.
func Resolve[T any](rt Runtime) (T, error) {
	var zero T
	typ := reflect.TypeFor[T]()
	reg, module, err := servicesOf(rt, "Resolve", typ)
	if err != nil {
		return zero, err
	}
	v, provider, found, hint := reg.lookup(typ)
	if !found {
		return zero, fmt.Errorf("%w: module %q resolves %s%s", ErrNotProvided, module, typ, hint)
	}
	reg.warnUndeclared(rt, module, provider, typ)
	out, ok := v.(T)
	if !ok {
		// Unreachable: provide stores a T under T's reflect.Type.
		return zero, fmt.Errorf("nucleus: Resolve[%s]: provided value has type %T", typ, v)
	}
	return out, nil
}

// serviceHost is the unexported view Provide and Resolve type-assert on a
// Runtime: the framework's runtime implements it, carrying the registry the
// whole application shares and the name of the module it was handed to.
type serviceHost interface {
	serviceRegistry() (*serviceRegistry, string)
}

func servicesOf(rt Runtime, op string, typ reflect.Type) (*serviceRegistry, string, error) {
	host, ok := rt.(serviceHost)
	if ok {
		if reg, module := host.serviceRegistry(); reg != nil {
			return reg, module, nil
		}
	}
	return nil, "", fmt.Errorf("nucleus: %s[%s]: the runtime was not handed out by a running application — call it with the Runtime a module receives in OnStart", op, typ)
}

// serviceRegistry is the table of provided values one application shares
// across its modules' runtimes. It is written while modules start and only
// read once they all have (sealed).
type serviceRegistry struct {
	mu      sync.RWMutex
	entries map[reflect.Type]providedValue
	sealed  bool
	// deps holds each module's declared DependsOn (by Name), for the
	// undeclared-dependency warning.
	deps   map[string][]string
	warned map[string]bool
}

type providedValue struct {
	value  any
	module string
}

func newServiceRegistry(deps map[string][]string) *serviceRegistry {
	return &serviceRegistry{
		entries: map[reflect.Type]providedValue{},
		deps:    deps,
		warned:  map[string]bool{},
	}
}

func (r *serviceRegistry) provide(typ reflect.Type, v any, module string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sealed {
		return fmt.Errorf("nucleus: Provide[%s]: module %q provides after every module has started — provide values in OnStart", typ, module)
	}
	if prev, dup := r.entries[typ]; dup {
		return fmt.Errorf("%w: module %q provides %s, which module %q already provides — give the second value its own named type",
			ErrAlreadyProvided, module, typ, prev.module)
	}
	r.entries[typ] = providedValue{value: v, module: module}
	return nil
}

// seal closes the registry to Provide. Called once every module's OnStart
// has returned.
func (r *serviceRegistry) seal() {
	r.mu.Lock()
	r.sealed = true
	r.mu.Unlock()
}

// lookup returns the value provided under typ and its provider, or — when
// there is none — a hint for the error message.
func (r *serviceRegistry) lookup(typ reflect.Type) (any, string, bool, string) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if pv, ok := r.entries[typ]; ok {
		return pv.value, pv.module, true, ""
	}
	// A value provided under a related type is the likeliest mistake:
	// Provide(rt, impl) instead of Provide[Iface](rt, impl).
	var related []string
	for et, pv := range r.entries {
		if et.AssignableTo(typ) || typ.AssignableTo(et) {
			related = append(related, fmt.Sprintf("module %q provides it as %s — provide it with nucleus.Provide[%s] or resolve %s", pv.module, et, typ, et))
		}
	}
	sort.Strings(related)
	if len(related) > 0 {
		return nil, "", false, ": " + strings.Join(related, "; ")
	}
	if r.sealed {
		return nil, "", false, ": every module has started and none provides it"
	}
	return nil, "", false, ": no module has provided it yet — the module that provides it must start first, so declare it in this module's DependsOn"
}

// warnUndeclared logs once per (module, type) when module resolves, during
// startup, a value from a provider it does not declare in DependsOn,
// directly or through its dependencies.
func (r *serviceRegistry) warnUndeclared(rt Runtime, module, provider string, typ reflect.Type) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sealed || provider == module || r.dependsOnLocked(module, provider) {
		return
	}
	key := module + "\x00" + typ.String()
	if r.warned[key] {
		return
	}
	r.warned[key] = true
	logger := slog.Default()
	if rt != nil {
		if l := rt.Logger(); l != nil {
			logger = l
		}
	}
	logger.Warn("nucleus: a module resolves a value from a module it does not declare in DependsOn; it is found only because that module happens to start first by name — declare the dependency",
		"module", module, "resolves", typ.String(), "provided_by", provider)
}

// dependsOnLocked reports whether module depends on target through its
// DependsOn declarations, transitively. Caller holds r.mu.
func (r *serviceRegistry) dependsOnLocked(module, target string) bool {
	seen := map[string]bool{}
	stack := append([]string(nil), r.deps[module]...)
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if cur == target {
			return true
		}
		if seen[cur] {
			continue
		}
		seen[cur] = true
		stack = append(stack, r.deps[cur]...)
	}
	return false
}

func isNilValue(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan, reflect.Interface, reflect.UnsafePointer:
		return rv.IsNil()
	}
	return false
}
