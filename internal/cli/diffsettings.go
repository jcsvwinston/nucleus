package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/app"
)

type settingDiff struct {
	Key     string `json:"key"`
	Default any    `json:"default"`
	Current any    `json:"current"`
	Changed bool   `json:"changed"`
}

type diffsettingsReport struct {
	Changed []settingDiff `json:"changed"`
	All     []settingDiff `json:"all,omitempty"`
}

func runDiffSettings(args []string, _ io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("diffsettings", flag.ContinueOnError)
	fs.SetOutput(stderr)

	configPath := fs.String("config", "", "Path to nucleus config file")
	showAll := fs.Bool("all", false, "Show all settings (including unchanged defaults)")
	asJSON := fs.Bool("json", false, "Print output as JSON")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if len(fs.Args()) > 0 {
		return fmt.Errorf("diffsettings does not accept positional arguments")
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}
	defaultCfg := app.DefaultConfig()
	diffs := diffConfig(defaultCfg, *cfg)

	changed := make([]settingDiff, 0, len(diffs))
	for _, item := range diffs {
		if item.Changed {
			changed = append(changed, item)
		}
	}

	if *asJSON {
		report := diffsettingsReport{
			Changed: changed,
		}
		if *showAll {
			report.All = diffs
		}
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}

	if *showAll {
		for _, item := range diffs {
			state := "unchanged"
			if item.Changed {
				state = "changed"
			}
			fmt.Fprintf(stdout, "%s\t%s\tcurrent=%s\tdefault=%s\n", item.Key, state, formatSettingValue(item.Current), formatSettingValue(item.Default))
		}
		return nil
	}

	if len(changed) == 0 {
		fmt.Fprintln(stdout, "No configuration differences from defaults")
		return nil
	}

	for _, item := range changed {
		fmt.Fprintf(stdout, "%s\t%s -> %s\n", item.Key, formatSettingValue(item.Default), formatSettingValue(item.Current))
	}
	return nil
}

func diffConfig(defaultCfg, currentCfg app.Config) []settingDiff {
	defaultMap := configToMap(defaultCfg)
	currentMap := configToMap(currentCfg)

	keys := make([]string, 0, len(defaultMap))
	for key := range defaultMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	diffs := make([]settingDiff, 0, len(keys))
	for _, key := range keys {
		def := defaultMap[key]
		cur := currentMap[key]
		diffs = append(diffs, settingDiff{
			Key:     key,
			Default: def,
			Current: cur,
			Changed: !reflect.DeepEqual(def, cur),
		})
	}
	return diffs
}

func configToMap(cfg app.Config) map[string]any {
	v := reflect.ValueOf(cfg)
	t := reflect.TypeOf(cfg)
	out := make(map[string]any, t.NumField())

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		key := strings.TrimSpace(field.Tag.Get("koanf"))
		if key == "" {
			key = strings.ToLower(field.Name)
		}

		val := v.Field(i).Interface()
		out[key] = normalizeSettingValue(val)
	}
	return out
}

func normalizeSettingValue(v any) any {
	switch vv := v.(type) {
	case time.Duration:
		return vv.String()
	}
	// A nil slice/map and an empty one are the same setting: the loader
	// materialises empty collections where the struct default leaves them
	// nil, and reflect.DeepEqual treats that as a difference — which made
	// diffsettings print no-differences like `auth_federated [] -> []`
	// (NC-13). Collapse both to the typed zero value before comparing.
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice, reflect.Map:
		if rv.Len() == 0 {
			return reflect.Zero(rv.Type()).Interface()
		}
	}
	return v
}

func formatSettingValue(v any) string {
	switch vv := v.(type) {
	case string:
		if vv == "" {
			return `""`
		}
		return vv
	default:
		return fmt.Sprint(vv)
	}
}
