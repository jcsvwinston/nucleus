// Package nucleus — validate_module_config.go implements ADR-010 §2 layer 5
// (module-specific configuration binding + validation), the fifth and final
// layer of the FromConfigFile validator. Where layers 2–4 validate app.Config,
// layer 5 binds each mounted module's `modules.<name>.*` subtree into the
// module author's typed Config, fills still-zero fields from `default:` struct
// tags, and validates the result against its `validate:` tags
// (go-playground/validator, via pkg/validate).
//
// Like layers 3 and 4 it runs on BOTH surfaces: the builder path
// (FromConfigFile → Build → Run) supplies the file subtree captured on
// App.moduleConfigsRaw, while the direct-struct Run(App{}) path has no file and
// so only applies defaults + validation to the programmatically-set Config.
// Binding happens at Run time — not in FromConfigFile or Mount — because those
// two may be called in either order, and only at Run are both the merged config
// and the full module set known (the same reason layer 4's validateModuleRequires
// runs at Run).
package nucleus

import (
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"sort"
	"strconv"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/validate"
	"github.com/knadh/koanf/v2"
)

// ErrInvalidModuleConfig is returned when a module's configuration cannot be
// bound or fails validation (ADR-010 §2 layer 5). The wrapped message names the
// offending module and the failing stage (binding, defaults, or validation).
var ErrInvalidModuleConfig = errors.New("nucleus: invalid module configuration")

// bindModuleConfigs applies ADR-010 §2 layer 5 to every mounted module: it binds
// the module's `modules.<name>.*` subtree (when a config file supplied one) into
// the typed Config, applies `default:` tags, and validates `validate:` tags. The
// bound specs replace App.Modules so the downstream startup sequence
// (registerModuleModels, OnStart, Routes) observes the resolved config.
//
// It writes a fresh map rather than mutating App.Modules in place so the
// direct-struct caller's own map is never altered. Modules are processed in
// sorted name order so the first reported error is deterministic. A module
// whose ModuleSpec is not the framework's own wrapper (i.e. does not implement
// moduleConfigBinder) is passed through unchanged — there is no typed config to
// bind.
func bindModuleConfigs(a *App) error {
	if a == nil || len(a.Modules) == 0 {
		// Still surface config aimed at modules that were never mounted.
		warnUnmountedModuleConfigs(a)
		if a != nil {
			a.moduleConfigsRaw = nil
		}
		return nil
	}

	names := make([]string, 0, len(a.Modules))
	for name := range a.Modules {
		names = append(names, name)
	}
	sort.Strings(names)

	bound := make(map[string]ModuleSpec, len(a.Modules))
	for _, name := range names {
		spec := a.Modules[name]
		binder, ok := spec.(moduleConfigBinder)
		if !ok {
			bound[name] = spec
			continue
		}
		var raw *koanf.Koanf
		if a.moduleConfigsRaw != nil {
			raw = a.moduleConfigsRaw[name]
		}
		b, err := binder.bindConfig(raw)
		if err != nil {
			return err
		}
		bound[name] = b
	}
	a.Modules = bound

	warnUnmountedModuleConfigs(a)

	// Release the retained raw subtrees once bound: they hold module config
	// (potentially secrets, e.g. a stripe_key) in cleartext, and nothing reads
	// them after binding. Run() holds App by value for the whole process
	// lifetime, so dropping the reference here lets the sub-koanf maps be GC'd
	// rather than lingering for the life of the server.
	a.moduleConfigsRaw = nil
	return nil
}

// warnUnmountedModuleConfigs emits a WARN for every `modules.<name>.*` block
// whose module was never mounted — a likely typo or a forgotten Mount. The
// config is ignored rather than rejected: a strict reject would couple Run to
// the FromConfigFile unknown-fields mode, which is not threaded this far, and
// would break overlay files that legitimately pre-stage config for modules a
// given binary does not mount.
func warnUnmountedModuleConfigs(a *App) {
	if a == nil || len(a.moduleConfigsRaw) == 0 {
		return
	}
	names := make([]string, 0, len(a.moduleConfigsRaw))
	for name := range a.moduleConfigsRaw {
		if _, mounted := a.Modules[name]; !mounted {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		slog.Default().Warn("nucleus: configuration provided for an unmounted module; ignoring",
			"module", name,
			"hint", fmt.Sprintf("Mount the module or remove its modules.%s.* config block", name),
		)
	}
}

// validateModuleConfigValue runs the module Config through pkg/validate so its
// `validate:` struct tags are enforced (ADR-010 §2 layer 5). Non-struct configs
// (e.g. struct{}, a map-based Config) carry no validate tags, so they are
// skipped rather than passed to go-playground/validator, which errors on a
// non-struct argument.
func validateModuleConfigValue(name string, cfg any) error {
	rv := reflect.ValueOf(cfg)
	for rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return nil
	}
	if err := validate.Validate(cfg); err != nil {
		return fmt.Errorf("%w: module %q: %w", ErrInvalidModuleConfig, name, err)
	}
	return nil
}

// applyDefaults fills zero-valued, settable struct fields from their `default:`
// struct tag. It is the `default:` half of ADR-010 §2 layer 5, implemented as a
// small reflection pass rather than pulling in a defaults dependency
// (stdlib-first; a new dep would require an ADR). ptr must be a non-nil pointer
// to a struct; anything else is a no-op (a module Config that is not a struct
// carries no default tags).
//
// A field is filled only when it is currently the zero value, so an
// author-supplied or file-bound value always wins over the tag default. The
// consequence — a field intentionally set to its zero value cannot be
// distinguished from "unset" — is the standard limitation of zero-value
// defaulting and is documented on the module-config surface.
func applyDefaults(ptr any) error {
	v := reflect.ValueOf(ptr)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		return nil
	}
	return applyDefaultsValue(v.Elem())
}

func applyDefaultsValue(v reflect.Value) error {
	if v.Kind() != reflect.Struct {
		return nil
	}
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.PkgPath != "" { // unexported field — not settable
			continue
		}
		fv := v.Field(i)

		// Recurse into nested structs (but not time.Time-like structs, which
		// have no exported fields to default and are better handled as scalars
		// via a default: tag on the field itself). time.Duration is an int64,
		// not a struct, so it is handled by setFromString below.
		//
		// Pointer fields: a non-nil pointer-to-struct is recursed into; a nil
		// pointer is left as a no-op here and falls through to the tag block,
		// where a default: tag on a pointer field surfaces a loud
		// "unsupported …" error rather than being silently ignored.
		switch {
		case fv.Kind() == reflect.Struct && fv.Type() != reflect.TypeOf(time.Time{}):
			if err := applyDefaultsValue(fv); err != nil {
				return err
			}
			continue
		case fv.Kind() == reflect.Ptr && !fv.IsNil() && fv.Elem().Kind() == reflect.Struct:
			if err := applyDefaultsValue(fv.Elem()); err != nil {
				return err
			}
			continue
		}

		tag, ok := field.Tag.Lookup("default")
		if !ok {
			continue
		}
		if !fv.CanSet() || !fv.IsZero() {
			continue
		}
		if err := setFromString(fv, tag); err != nil {
			return fmt.Errorf("field %s: %w", field.Name, err)
		}
	}
	return nil
}

// setFromString parses the `default:` tag string into the target field's type.
// time.Duration is special-cased (human strings like "30s"); the remaining
// scalar kinds parse via strconv. Unsupported kinds (slices, maps, structs)
// return an error so a mis-tagged field fails loud at boot rather than silently.
func setFromString(fv reflect.Value, s string) error {
	if fv.Type() == reflect.TypeOf(time.Duration(0)) {
		d, err := time.ParseDuration(s)
		if err != nil {
			return fmt.Errorf("invalid duration default %q: %w", s, err)
		}
		fv.SetInt(int64(d))
		return nil
	}

	switch fv.Kind() {
	case reflect.String:
		fv.SetString(s)
	case reflect.Bool:
		b, err := strconv.ParseBool(s)
		if err != nil {
			return fmt.Errorf("invalid bool default %q: %w", s, err)
		}
		fv.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid int default %q: %w", s, err)
		}
		fv.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid uint default %q: %w", s, err)
		}
		fv.SetUint(n)
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return fmt.Errorf("invalid float default %q: %w", s, err)
		}
		fv.SetFloat(f)
	default:
		return fmt.Errorf("field of kind %s does not support a `default:` tag", fv.Kind())
	}
	return nil
}
