// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// QCD-FW-4 (external coverage demo, 2026-08-17): the QCD-FW-2 bucket
// provisioning fix was UNREACHABLE from nucleus.yml. pkg/storage gained
// `s3.create_bucket_if_missing` and the S3 constructor's missing-bucket
// error recommends exactly that key — but the app-level mirror struct
// (Config.Storage.S3 in this package) never gained the field, so the strict
// config validator (DX-13) rejects the very key the startup error
// prescribes. The circular repro, executed against real MinIO:
//
//	go run .   # missing bucket
//	→ "set storage.s3.create_bucket_if_missing: true to provision it"  EXIT=1
//	go run .   # after adding that exact key to nucleus.yml
//	→ "unknown configuration key(s): storage.s3.create_bucket_if_missing"  EXIT=1
//
// Root cause is systemic: TWO structs describe the same configuration
// (pkg/storage's own Config and this package's mirror) with no guard
// keeping them in sync — under strict config validation every future
// divergence becomes another advertised-but-rejected key. Hence the parity
// test below, which walks both sides by reflection.
package app

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/storage"
)

// The exact circular repro: the key the startup error recommends must load
// (not be rejected as unknown) and must reach the storage.Config that
// app.New hands to storage.New.
func TestCreateBucketIfMissingReachesStorageConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "nucleus.yml")
	yml := `storage:
  provider: s3
  s3:
    endpoint: http://127.0.0.1:9000
    bucket: attachments-v18
    access_key_id: minioadmin
    secret_access_key: minioadmin
    use_path_style: true
    create_bucket_if_missing: true
`
	if err := os.WriteFile(cfgPath, []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("the key the startup error recommends must be accepted by the config loader (QCD-FW-4), got: %v", err)
	}

	sc := cfg.toStorageConfig()
	if !sc.S3.CreateBucketIfMissing {
		t.Fatal("storage.s3.create_bucket_if_missing loaded but did not propagate into storage.Config (QCD-FW-4)")
	}
}

// The same key must be reachable through the environment grammar
// (NUCLEUS_STORAGE__S3__CREATE_BUCKET_IF_MISSING), which deserializes into
// the same mirror struct.
func TestCreateBucketIfMissingReachableViaEnv(t *testing.T) {
	t.Setenv("NUCLEUS_STORAGE__S3__CREATE_BUCKET_IF_MISSING", "true")
	dir := t.TempDir()
	oldWd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.toStorageConfig().S3.CreateBucketIfMissing {
		t.Fatal("NUCLEUS_STORAGE__S3__CREATE_BUCKET_IF_MISSING=true did not reach storage.Config (QCD-FW-4)")
	}
}

// storageMirrorExclusions lists koanf keys of pkg/storage config structs that
// the app mirror deliberately does NOT expose. Every entry needs a reason —
// an entry without one is just QCD-FW-4 with permission. Keys are
// "<section>.<koanf-tag>".
var storageMirrorExclusions = map[string]string{
	// The mirror flattens credential fields to direct string values (the
	// documented app-config stance: "Direct value (use env vars at OS
	// level)"). CredentialSource's env_var/file/secret_manager indirection is
	// available by constructing storage.Config directly with pkg/storage.
	"s3.session_token": "credential-source field; the app mirror only carries direct-value credentials",
	"gcs.credentials":  "credential-source field; GCS uses Application Default Credentials from the app path",
}

// koanfKeys returns the koanf tag set of a struct type, prefixed.
func koanfKeys(t reflect.Type, prefix string) map[string]struct{} {
	out := map[string]struct{}{}
	for i := 0; i < t.NumField(); i++ {
		tag := strings.TrimSpace(t.Field(i).Tag.Get("koanf"))
		if tag == "" || tag == "-" {
			continue
		}
		out[prefix+tag] = struct{}{}
	}
	return out
}

// TestStorageConfigMirrorParity walks every mirrored storage section by
// reflection: a koanf key present in pkg/storage's config structs must exist
// in the app mirror (or carry an explicit exclusion with a reason), and every
// mirror key must exist on the storage side. This is the guard whose absence
// produced QCD-FW-4 — with strict config validation, an unmirrored key is an
// advertised-but-rejected key.
func TestStorageConfigMirrorParity(t *testing.T) {
	appStorage := reflect.TypeOf(Config{}.Storage)

	sections := []struct {
		name        string
		storageSide reflect.Type
		mirrorField string
	}{
		{"s3", reflect.TypeOf(storage.S3Config{}), "S3"},
		{"gcs", reflect.TypeOf(storage.GCSConfig{}), "GCS"},
		{"azure", reflect.TypeOf(storage.AzureConfig{}), "Azure"},
		{"local", reflect.TypeOf(storage.LocalConfig{}), "Local"},
		{"cleanup", reflect.TypeOf(storage.CleanupConfig{}), "Cleanup"},
	}

	for _, sec := range sections {
		mirrorField, ok := appStorage.FieldByName(sec.mirrorField)
		if !ok {
			t.Errorf("app mirror has no %s section for storage.%s", sec.mirrorField, sec.name)
			continue
		}
		storageKeys := koanfKeys(sec.storageSide, sec.name+".")
		mirrorKeys := koanfKeys(mirrorField.Type, sec.name+".")

		for key := range storageKeys {
			if _, mirrored := mirrorKeys[key]; mirrored {
				continue
			}
			reason, excluded := storageMirrorExclusions[key]
			if !excluded || strings.TrimSpace(reason) == "" {
				t.Errorf("storage key %q exists in pkg/storage but the app mirror does not expose it — with strict config validation this is an advertised-but-rejected key (QCD-FW-4). Mirror it in pkg/app/config.go (and toStorageConfig), or add an explicit exclusion WITH a reason.", key)
			}
		}
		for key := range mirrorKeys {
			if _, exists := storageKeys[key]; !exists {
				t.Errorf("app mirror key %q has no counterpart in pkg/storage — the mirror drifted ahead", key)
			}
		}
		// Exclusions must stay honest: an excluded key that IS mirrored now
		// is a stale entry.
		for key, reason := range storageMirrorExclusions {
			if !strings.HasPrefix(key, sec.name+".") {
				continue
			}
			if _, mirrored := mirrorKeys[key]; mirrored {
				t.Errorf("exclusion for %q (%s) is stale: the key is mirrored now — delete the entry", key, reason)
			}
		}
	}

	// Fail also if an exclusion references a key that no longer exists on the
	// storage side (rot in the exclusion list itself).
	all := map[string]struct{}{}
	for _, sec := range sections {
		for k := range koanfKeys(sec.storageSide, sec.name+".") {
			all[k] = struct{}{}
		}
	}
	for key := range storageMirrorExclusions {
		if _, ok := all[key]; !ok {
			t.Errorf("exclusion %q references a key that does not exist in pkg/storage", key)
		}
	}
}
