// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Package configbind is the single place a merged koanf tree becomes a
// framework config struct. Both full-config consumers — pkg/app's
// LoadConfig (the CLI path) and pkg/nucleus's fluent loader — bind through
// here so their decoding can never diverge; it is internal on purpose (the
// dependency firewall forbids koanf types on the stable API, ADR-015).
//
// On top of koanf's default hooks (duration parsing, weak typing, comma
// slices) it promotes a plain string into a storage.CredentialSource
// {value: …}, so `access_key_id: "literal"` keeps binding while the full
// env_var/file/secret_manager shape the README promises is loadable too —
// QCD-FW-4's class was exactly a config shape the docs promise but the
// decoder rejects.
package configbind

import (
	"reflect"

	"github.com/go-viper/mapstructure/v2"
	"github.com/jcsvwinston/nucleus/pkg/storage"
	"github.com/knadh/koanf/v2"
)

// Unmarshal binds the koanf tree at the root into target with the
// framework's decode hooks.
func Unmarshal(k *koanf.Koanf, target any) error {
	return k.UnmarshalWithConf("", target, koanf.UnmarshalConf{
		Tag: "koanf",
		DecoderConfig: &mapstructure.DecoderConfig{
			DecodeHook: mapstructure.ComposeDecodeHookFunc(
				stringToCredentialSourceHook,
				mapstructure.StringToTimeDurationHookFunc(),
				mapstructure.StringToSliceHookFunc(","),
				mapstructure.TextUnmarshallerHookFunc(),
			),
			Result:           target,
			WeaklyTypedInput: true,
		},
	})
}

// stringToCredentialSourceHook lets a scalar YAML value fill a
// CredentialSource as its literal Value — backward compatibility for every
// config written before the credential shape existed on these keys.
func stringToCredentialSourceHook(from reflect.Type, to reflect.Type, data interface{}) (interface{}, error) {
	if from.Kind() != reflect.String || to != reflect.TypeOf(storage.CredentialSource{}) {
		return data, nil
	}
	return storage.CredentialSource{Value: data.(string)}, nil
}
