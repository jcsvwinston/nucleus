// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// QCD-FW-4 (external coverage demo, 2026-08-17): the QCD-FW-2 bucket
// provisioning fix was UNREACHABLE from nucleus.yml. pkg/storage gained
// `s3.create_bucket_if_missing` and the S3 constructor's missing-bucket
// error recommends exactly that key — but the app-level mirror struct
// (Config.Storage in this package) never gained the field, so the strict
// config validator (DX-13) rejects the very key the startup error
// prescribes.
//
// Root cause is systemic: TWO structs describe the same configuration
// (pkg/storage's own Config and this package's mirror) with no guard
// keeping them in sync — under strict config validation every future
// divergence becomes another advertised-but-rejected key. The first guard
// (v1.8.1) walked one level flat and compared only KEY NAMES, which is why
// two more divergences of the same class lived on for months: the
// credential fields were `string` in the mirror while pkg/storage declares
// storage.CredentialSource — so `storage.s3.access_key_id.env_var`, the
// shape the README promises for every sensitive value, was rejected as an
// unknown key — and `s3.session_token`/`gcs.credentials` were missing
// outright (both hidden behind "reasoned" exclusions that were really
// deferrals). This guard walks RECURSIVELY and compares TYPES; the
// exclusion list is gone because nothing is excluded any more.
package app

import (
	"reflect"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/storage"
)

// koanfTypedKeys returns koanf-path → field type for a struct type,
// recursing into nested structs EXCEPT storage.CredentialSource, which is a
// leaf shape shared by both sides (recursing into it would just re-compare
// its own fields four times per credential).
func koanfTypedKeys(t reflect.Type, prefix string) map[string]reflect.Type {
	out := map[string]reflect.Type{}
	credentialLeaf := reflect.TypeOf(storage.CredentialSource{})
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := strings.TrimSpace(f.Tag.Get("koanf"))
		if tag == "" || tag == "-" {
			continue
		}
		key := prefix + tag
		if f.Type.Kind() == reflect.Struct && f.Type != credentialLeaf {
			for k, v := range koanfTypedKeys(f.Type, key+".") {
				out[k] = v
			}
			continue
		}
		out[key] = f.Type
	}
	return out
}

// typesEquivalent accepts identical types, and named string/bool/int
// aliases over the same kind (pkg/storage uses Visibility/ProviderType
// where the mirror deliberately stays `string`: the mirror is the
// yaml-facing shape, the alias is the domain type).
func typesEquivalent(mirror, storageSide reflect.Type) bool {
	if mirror == storageSide {
		return true
	}
	return mirror.Kind() == storageSide.Kind() &&
		(mirror.Kind() == reflect.String || mirror.Kind() == reflect.Bool ||
			mirror.Kind() == reflect.Int || mirror.Kind() == reflect.Int64)
}

// TestStorageConfigMirrorParity walks pkg/storage.Config and the app mirror
// (Config.Storage) recursively: every koanf key must exist on BOTH sides
// with an equivalent type. A key missing from the mirror is an
// advertised-but-rejected key (QCD-FW-4); a mis-typed one is the same lie
// in a subtler shape (the credential-source class); a mirror-only key is a
// key the runtime silently ignores.
func TestStorageConfigMirrorParity(t *testing.T) {
	storageKeys := koanfTypedKeys(reflect.TypeOf(storage.Config{}), "storage.")
	mirrorKeys := koanfTypedKeys(reflect.TypeOf(Config{}.Storage), "storage.")

	for key, st := range storageKeys {
		mt, mirrored := mirrorKeys[key]
		if !mirrored {
			t.Errorf("storage key %q exists in pkg/storage but the app mirror does not expose it — with strict config validation this is an advertised-but-rejected key (QCD-FW-4). Mirror it in pkg/app/config.go AND thread it through toStorageConfig.", key)
			continue
		}
		if !typesEquivalent(mt, st) {
			t.Errorf("storage key %q is %v in pkg/storage but %v in the app mirror — the yaml shapes diverge, so the documented form of the key is rejected at load (the credential-source class).", key, st, mt)
		}
	}
	for key := range mirrorKeys {
		if _, exists := storageKeys[key]; !exists {
			t.Errorf("mirror key %q does not exist in pkg/storage — the runtime ignores it silently. Remove it or add it to pkg/storage.", key)
		}
	}
}

// TestToStorageConfigThreadsEveryMirrorField pins the OTHER half of the
// mirror contract: binding a fully-populated yaml-shaped mirror and
// converting it must not drop a value on the floor. The credential fields
// carry distinct markers so a copy-paste slip (Azure key into Azure name…)
// shows its exact location.
func TestToStorageConfigThreadsEveryMirrorField(t *testing.T) {
	c := DefaultConfig()
	c.Storage.Provider = "s3"
	c.Storage.S3 = S3StorageSpec{
		Endpoint:              "http://minio:9000",
		Bucket:                "b",
		Region:                "eu-south-2",
		AccessKeyID:           storage.CredentialSource{EnvVar: "AK"},
		SecretAccessKey:       storage.CredentialSource{File: "/run/secret"},
		SessionToken:          storage.CredentialSource{Value: "tok"},
		UsePathStyle:          true,
		PublicBucket:          "pb",
		CreateBucketIfMissing: true,
	}
	c.Storage.GCS = GCSStorageSpec{Bucket: "g", CredentialsSource: storage.CredentialSource{File: "/sa.json"}, PublicBucket: "gp"}
	c.Storage.Azure = AzureStorageSpec{AccountName: storage.CredentialSource{Value: "an"}, AccountKey: storage.CredentialSource{EnvVar: "AZK"}, Container: "c", PublicContainer: "pc"}

	sc := c.toStorageConfig()

	if sc.S3.AccessKeyID.EnvVar != "AK" || sc.S3.SecretAccessKey.File != "/run/secret" || sc.S3.SessionToken.Value != "tok" {
		t.Errorf("S3 credential sources did not thread through: %+v", sc.S3)
	}
	if sc.GCS.CredentialsSource.File != "/sa.json" {
		t.Errorf("GCS credentials did not thread through: %+v", sc.GCS)
	}
	if sc.Azure.AccountName.Value != "an" || sc.Azure.AccountKey.EnvVar != "AZK" {
		t.Errorf("Azure credential sources did not thread through: %+v", sc.Azure)
	}
	if !sc.S3.CreateBucketIfMissing || !sc.S3.UsePathStyle || sc.S3.Region != "eu-south-2" {
		t.Errorf("S3 scalars did not thread through: %+v", sc.S3)
	}
}
