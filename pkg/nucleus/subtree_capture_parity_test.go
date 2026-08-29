package nucleus

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/router/interceptor"
)

// TestSubtreeCapture_BothPathsAgree pins that the fluent builder path and
// app.LoadConfig capture the SAME provider subtrees from the same file.
//
// app.LoadConfig captures three: storage, auth (backends AND federated
// instances) and interceptors, with the comment "capturing on only one of
// the two paths is how 'the same file, two verdicts' comes back". The
// builder path captured storage and auth-backends only:
//
//   - `interceptors.<name>.*` was not captured at all, so every registered
//     interceptor was built with its `default:` tags instead of what the
//     file declared.
//   - `auth.<instance>.*` for a FEDERATED instance was missed, because the
//     builder passed only cfg.AuthBackends and not the federated names.
//
// Both are silent: the interceptor mounts, the instance exists, and neither
// carries the settings the operator wrote. This is the path the README
// documents and the one the coverage demo boots through.
func TestSubtreeCapture_BothPathsAgree(t *testing.T) {
	// The subtree is only a legitimate key because the name is registered;
	// the unknown-key guard exempts it on that basis.
	if err := interceptor.Register("audittest", func(interceptor.Config) (interceptor.Interceptor, error) {
		return nil, nil
	}); err != nil {
		t.Fatalf("register interceptor: %v", err)
	}
	t.Cleanup(func() { interceptor.Unregister("audittest") })

	dir := t.TempDir()
	path := filepath.Join(dir, "nucleus.yml")
	const yaml = `
http_interceptors: [audittest]
interceptors:
  audittest:
    sink: stdout
    verbose: true
auth_federated:
  - name: oktatest
    client_id: cid
auth:
  oktatest:
    issuer: https://example.test
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	viaCLI, err := app.LoadConfig(path)
	if err != nil {
		t.Fatalf("app.LoadConfig: %v", err)
	}
	viaBuilder, _, err := loadFromFilesWithModules([]string{path}, configLoadOptions{})
	if err != nil {
		t.Fatalf("loadFromFilesWithModules: %v", err)
	}

	if !reflect.DeepEqual(viaBuilder.InterceptorConfig, viaCLI.InterceptorConfig) {
		t.Errorf("interceptors.<name>.* differs between the two paths:\n  builder = %v\n  CLI     = %v",
			viaBuilder.InterceptorConfig, viaCLI.InterceptorConfig)
	}
	if !reflect.DeepEqual(viaBuilder.AuthBackendConfig, viaCLI.AuthBackendConfig) {
		t.Errorf("auth.<name>.* differs between the two paths:\n  builder = %v\n  CLI     = %v",
			viaBuilder.AuthBackendConfig, viaCLI.AuthBackendConfig)
	}

	// And the values actually declared must be there, or the test could pass
	// on two paths that are equally blind.
	if got := viaBuilder.InterceptorConfig["audittest"]["sink"]; got != "stdout" {
		t.Errorf("interceptors.audittest.sink = %v, want stdout", got)
	}
	if got := viaBuilder.AuthBackendConfig["oktatest"]["issuer"]; got != "https://example.test" {
		t.Errorf("auth.oktatest.issuer = %v, want https://example.test", got)
	}
}
