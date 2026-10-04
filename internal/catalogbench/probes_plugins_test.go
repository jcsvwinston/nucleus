// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package catalogbench

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/internal/cli"
	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/mail"
	"github.com/jcsvwinston/nucleus/pkg/plugins"
)

// The plugin probes measure what an application author can write to extend
// the framework, and what the framework does with it: an external
// `nucleus-plugin-*` binary speaking the envelope, an external `nucleus-*`
// command, an in-process module or provider, and the examples and templates
// that make writing one a copy rather than a reading of the reference.
//
// The plugins the probes run are shell scripts: the envelope is the
// contract, and a script is the smallest program that speaks it.

// pluginScript writes `nucleus-plugin-<provider>` into dir: it advertises
// caps, and appends every request envelope it receives to capture.
func pluginScript(t *testing.T, dir, provider, capture string, caps ...string) string {
	t.Helper()
	quoted := make([]string, len(caps))
	for i, c := range caps {
		quoted[i] = `"` + c + `"`
	}
	path := filepath.Join(dir, plugins.GenericBinaryPrefix+provider)
	writeExecutable(t, path, `#!/bin/sh
if [ "$1" = "capabilities" ]; then
  echo '{"capabilities":[`+strings.Join(quoted, ",")+`]}'
  exit 0
fi
cat >> "`+capture+`"
echo >> "`+capture+`"
echo '{"version":"v1","request_id":"catalogbench","ok":true,"output":{"accepted":true}}'
`)
	return path
}

// withPath puts dir first on PATH for the rest of the test.
func withPath(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// envelopes reads what a script plugin captured.
func envelopes(t *testing.T, capture string) []plugins.RequestEnvelope {
	t.Helper()
	raw, _ := os.ReadFile(capture)
	var out []plugins.RequestEnvelope
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var env plugins.RequestEnvelope
		if err := json.Unmarshal([]byte(line), &env); err == nil {
			out = append(out, env)
		}
	}
	return out
}

// examplePluginDir matches a directory that holds an example plugin, under
// the names a repository gives one.
var examplePluginDir = regexp.MustCompile(`(?i)(^nucleus-plugin-|^(example|sample)[-_]?plugins?$|^plugins?[-_]?(example|sample)s?$)`)

// findExamplePlugins walks the repository for an example external plugin:
// a `package main` under a directory named like one, or under an examples
// or testdata `plugins` directory.
func findExamplePlugins(t *testing.T) []string {
	t.Helper()
	root := repoRoot(t)
	var out []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		if skipDir(d.Name()) || d.Name() == "website" {
			return filepath.SkipDir
		}
		rel, _ := filepath.Rel(root, path)
		slashed := filepath.ToSlash(rel)
		if !examplePluginDir.MatchString(d.Name()) &&
			!strings.Contains(slashed, "examples/plugins/") && !strings.Contains(slashed, "testdata/plugins/") {
			return nil
		}
		files, _ := filepath.Glob(filepath.Join(path, "*.go"))
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			if src, _ := os.ReadFile(f); regexp.MustCompile(`(?m)^package main\b`).Match(src) {
				out = append(out, slashed)
				break
			}
		}
		return nil
	})
	sort.Strings(out)
	return out
}

// buildPlugin builds an example plugin directory into dir as
// nucleus-plugin-<provider>.
func buildPlugin(t *testing.T, rel, dir string) (string, string, error) {
	t.Helper()
	root := repoRoot(t)
	provider := strings.TrimPrefix(filepath.Base(rel), plugins.GenericBinaryPrefix)
	out := filepath.Join(dir, plugins.GenericBinaryPrefix+provider)
	src := filepath.Join(root, rel)
	var log string
	var err error
	if _, statErr := os.Stat(filepath.Join(src, "go.mod")); statErr == nil {
		log, err = goRun(src, "build", "-o", out, ".")
	} else {
		log, err = goRun(root, "build", "-o", out, "./"+rel)
	}
	return provider, log, err
}

// samplePayload is a request body for a capability's documented schema.
func samplePayload(capability string) any {
	switch capability {
	case plugins.CapabilityMailSend:
		return plugins.MailSendPayload{From: "bench@example.test", To: []string{"dev@example.test"}, Subject: "catalogbench", Body: "hello"}
	case plugins.CapabilityQueuePublish:
		return plugins.QueuePublishPayload{Topic: "catalogbench", Body: json.RawMessage(`{"hello":"world"}`)}
	case plugins.CapabilityWebhookDeliver:
		return plugins.WebhookDeliverPayload{URL: "https://example.test/hook", Method: "POST", Body: `{"hello":"world"}`}
	}
	return map[string]string{"hello": "world"}
}

// EX-01 — an example external plugin ships as a tested fixture: it builds,
// `nucleus plugin test --execute` passes on it, and it answers a real
// request envelope.
func probeExamplePlugin(t *testing.T, e *env) verdict {
	requireShell(t)
	found := findExamplePlugins(t)
	if len(found) == 0 {
		t.Log("no example plugin in the repository: no `package main` under a nucleus-plugin-*, example-plugin, " +
			"plugin-example, examples/plugins/ or testdata/plugins/ directory")
		return absent
	}
	dir := t.TempDir()
	withPath(t, dir)
	verdictSoFar := present
	for _, rel := range found {
		provider, log, err := buildPlugin(t, rel, dir)
		if err != nil {
			t.Logf("%s does not build:\n%s", rel, log)
			verdictSoFar = partial
			continue
		}
		caps, err := plugins.ProbeCapabilities(context.Background(), filepath.Join(dir, plugins.GenericBinaryPrefix+provider), 5*time.Second)
		if err != nil || len(caps) == 0 {
			t.Logf("%s advertises no capability: %v", rel, err)
			verdictSoFar = partial
			continue
		}
		r := e.cli("plugin", "test", "--provider", provider, "--capability", caps[0], "--execute", "--json")
		var report struct {
			Status string `json:"status"`
		}
		_ = json.Unmarshal([]byte(r.stdout), &report)
		if report.Status != "ok" {
			t.Logf("nucleus plugin test on %s: %s", rel, firstLines(r.all(), 6))
			verdictSoFar = partial
			continue
		}
		req, err := plugins.NewRequestEnvelope(provider, caps[0], 5*time.Second, samplePayload(caps[0]), nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := plugins.ExecuteRequest(context.Background(), filepath.Join(dir, plugins.GenericBinaryPrefix+provider), req, 5*time.Second); err != nil {
			t.Logf("%s refuses a %s envelope: %v", rel, caps[0], err)
			verdictSoFar = partial
			continue
		}
		t.Logf("%s: builds, passes plugin test --execute, answers a %s envelope", rel, caps[0])
	}
	return verdictSoFar
}

// EX-02 — `mail.send` reaches an external plugin through the runtime: the
// mail sender the application builds for `mail_driver: <provider>` finds the
// binary on PATH and hands it the envelope.
func probeMailBridge(t *testing.T, _ *env) verdict {
	requireShell(t)
	dir := t.TempDir()
	capture := filepath.Join(dir, "capture.jsonl")
	pluginScript(t, dir, "catalogmail", capture, plugins.CapabilityMailSend)
	withPath(t, dir)
	sender, err := mail.NewSender(mail.Config{Driver: "catalogmail", Timeout: 5 * time.Second})
	if err != nil {
		t.Logf("mail_driver: catalogmail does not resolve to the plugin: %v", err)
		return absent
	}
	if err := sender.Send(context.Background(), mail.Message{
		From: "bench@example.test", To: []string{"dev@example.test"}, Subject: "catalogbench", Body: "hello",
	}); err != nil {
		t.Logf("Send through the plugin: %v", err)
		return partial
	}
	got := envelopes(t, capture)
	if len(got) != 1 || got[0].Capability != plugins.CapabilityMailSend || !strings.Contains(string(got[0].Payload), "catalogbench") {
		t.Logf("the plugin received %d envelope(s): %+v", len(got), got)
		return partial
	}
	t.Logf("the plugin received a %s %s envelope for provider %q", got[0].Version, got[0].Capability, got[0].Provider)
	return present
}

// bridgeTypes are the names the outbox's bridge configuration would give a
// plugin-backed bridge, the way the webhook one is named.
var bridgeTypes = []string{"plugin", "external", "exec", "nucleus-plugin"}

// probeOutboxPluginBridge asks the starter's outbox to deliver through an
// external plugin advertising capability, under every type name a plugin
// bridge would take, and reads what the application answers.
func probeOutboxPluginBridge(t *testing.T, e *env, capability string) verdict {
	requireShell(t)
	base := e.scaffold(t)
	dir := t.TempDir()
	capture := filepath.Join(dir, "capture.jsonl")
	pluginScript(t, dir, "catalogbridge", capture, capability)
	pathEnv := "PATH=" + dir + string(os.PathListSeparator) + os.Getenv("PATH")
	for _, typ := range bridgeTypes {
		config := starterConfig(t, base, "outbox:\n  enabled: true\n  bridges:\n    - name: catalogbridge\n      type: "+typ+
			"\n      config:\n        provider: catalogbridge\n        capability: "+capability+"\n")
		b := bootWith(t, base.bin, config, []string{pathEnv}, nil)
		switch {
		case !b.listening:
			t.Logf("type %q: the starter refuses: %s", typ, firstLines(lastLines(b.output, 3), 3))
		case strings.Contains(b.output, "unknown bridge type"):
			t.Logf("type %q: WARN unknown bridge type — the application boots and the bridge is dropped", typ)
		default:
			t.Logf("type %q is accepted by the outbox; grow this probe to enqueue a message and read the %s envelope the plugin receives", typ, capability)
			return present
		}
	}
	consumers := sourceMatches(t, "pkg", regexp.MustCompile(`plugins\.Capability`+map[string]string{
		plugins.CapabilityQueuePublish:   "QueuePublish",
		plugins.CapabilityWebhookDeliver: "WebhookDeliver",
	}[capability]))
	var outside []string
	for _, c := range consumers {
		if !strings.HasPrefix(filepath.ToSlash(c), "pkg/plugins/") {
			outside = append(outside, c)
		}
	}
	t.Logf("source outside pkg/plugins that names the %s capability: %v — the schema exists, nothing sends it", capability, outside)
	return absent
}

// EX-03 — `queue.publish` has a runtime bridge.
func probeQueuePublishBridge(t *testing.T, e *env) verdict {
	return probeOutboxPluginBridge(t, e, plugins.CapabilityQueuePublish)
}

// EX-04 — `webhook.deliver` has a runtime bridge.
func probeWebhookDeliverBridge(t *testing.T, e *env) verdict {
	return probeOutboxPluginBridge(t, e, plugins.CapabilityWebhookDeliver)
}

// registryCall matches source that extends the framework through one of its
// public registries, or declares a module or an extension.
var registryCall = regexp.MustCompile(`storage\.RegisterProvider\(|mail\.RegisterProvider\(|auth\.RegisterBackend\(|federated\.Register\(|exporter\.Register\(|secrets\.RegisterResolver\(|interceptor\.Register\(|nucleus\.Module\[|app\.Extension\b`)

// EX-05 — an in-process example — a provider registered through a public
// registry, or a module — ships as a fixture compiled and tested in CI.
func probeInProcessExample(t *testing.T, _ *env) verdict {
	root := repoRoot(t)
	var found []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		if skipDir(d.Name()) || d.Name() == "website" || d.Name() == "docs" {
			return filepath.SkipDir
		}
		if !regexp.MustCompile(`(?i)example|sample`).MatchString(d.Name()) {
			return nil
		}
		files, _ := filepath.Glob(filepath.Join(path, "*.go"))
		hasTest, registers := false, false
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				hasTest = true
				continue
			}
			if src, _ := os.ReadFile(f); registryCall.Match(src) {
				registers = true
			}
		}
		if hasTest && registers {
			rel, _ := filepath.Rel(root, path)
			found = append(found, filepath.ToSlash(rel))
		}
		return nil
	})
	if len(found) == 0 {
		t.Log("no directory named example/sample holds a tested provider, module or extension; the nearest things are " +
			"the first-party modules under providers/ and exporters/, which register the same way but are production " +
			"code with their own SDKs, not a starting point")
		return absent
	}
	for _, rel := range found {
		log, err := goRun(root, "test", "./"+rel)
		if err != nil {
			t.Logf("%s does not pass its own test:\n%s", rel, log)
			return partial
		}
		t.Logf("%s: registers through a public seam and passes its test", rel)
	}
	return present
}

// EX-06 — a community module template: the CLI writes a standalone module
// (its own go.mod) whose test checks it with nucleustest.CheckModule, and
// that test passes outside any workspace.
func probeCommunityTemplate(t *testing.T, e *env) verdict {
	attempts := [][]string{
		{"new", "community", "--template", "module", "--offline"},
		{"new", "community", "--template", "plugin", "--offline"},
		{"new", "community", "--template", "extension", "--offline"},
		{"generate", "plugin", "community"},
		{"generate", "extension", "community"},
		{"generate", "provider", "community"},
	}
	for _, args := range attempts {
		out := t.TempDir()
		r := e.cli(append(args, "--out", out)...)
		if r.code != 0 {
			t.Logf("nucleus %s: %s", strings.Join(args, " "), firstLines(r.stderr, 1))
			continue
		}
		var modDir string
		_ = filepath.WalkDir(out, func(path string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() && d.Name() == "go.mod" && modDir == "" {
				modDir = filepath.Dir(path)
			}
			return nil
		})
		if modDir == "" {
			t.Logf("nucleus %s wrote no standalone module", strings.Join(args, " "))
			return partial
		}
		if err := pinToCheckout(modDir, e.root); err != nil {
			t.Logf("pin the template to this checkout: %v", err)
		}
		checks := sourceMatchesIn(modDir, regexp.MustCompile(`nucleustest\.CheckModule`))
		if log, err := goRun(modDir, "mod", "tidy"); err != nil {
			skipIfOffline(t, log)
			t.Logf("the template does not resolve: %s", log)
			return partial
		}
		if log, err := goRun(modDir, "test", "./..."); err != nil {
			t.Logf("the template's test fails standalone:\n%s", log)
			return partial
		}
		if !checks {
			t.Log("the template's test does not call nucleustest.CheckModule")
			return partial
		}
		t.Logf("nucleus %s writes a standalone module whose CheckModule test passes", strings.Join(args, " "))
		return present
	}
	return absent
}

// sourceMatchesIn reports whether any test file under dir matches re.
func sourceMatchesIn(dir string, re *regexp.Regexp) bool {
	hit := false
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(path, "_test.go") {
			if src, _ := os.ReadFile(path); re.Match(src) {
				hit = true
			}
		}
		return nil
	})
	return hit
}

// commandScript writes an external command `nucleus-<name>` that echoes
// its arguments and its stdin and exits 3.
func commandScript(t *testing.T, dir, name string) {
	t.Helper()
	writeExecutable(t, filepath.Join(dir, "nucleus-"+name), "#!/bin/sh\necho \"args:$*\"\necho \"stdin:$(cat)\"\nexit 3\n")
}

// EX-07 — `nucleus <name>` dispatches to a `nucleus-<name>` binary on PATH,
// end to end: arguments, stdin, stdout and the exit code all cross.
func probeExternalCommandDispatch(t *testing.T, e *env) verdict {
	requireShell(t)
	dir := t.TempDir()
	commandScript(t, dir, "catalogcmd")
	withPath(t, dir)
	e.cliMu.Lock()
	var out, errb strings.Builder
	code := cli.Run([]string{"catalogcmd", "one", "two"}, strings.NewReader("hello"), &out, &errb)
	e.cliMu.Unlock()
	t.Logf("nucleus catalogcmd one two → exit %d, stdout %q", code, out.String())
	if strings.Contains(errb.String(), "unknown command") {
		return absent
	}
	if strings.Contains(out.String(), "args:one two") && strings.Contains(out.String(), "stdin:hello") && code == 3 {
		return present
	}
	return partial
}

// EX-08 — the external commands on PATH are discoverable from the CLI, the
// way `git help -a`, `kubectl plugin list` and `cargo --list` show theirs.
func probeExternalCommandsListed(t *testing.T, e *env) verdict {
	requireShell(t)
	dir := t.TempDir()
	commandScript(t, dir, "catalogcmd")
	withPath(t, dir)
	for _, args := range [][]string{{"--help"}, {"help"}, {"plugin", "list", "--json"}} {
		r := e.cli(args...)
		if strings.Contains(r.all(), "catalogcmd") {
			t.Logf("nucleus %s lists the external command", strings.Join(args, " "))
			return present
		}
		t.Logf("nucleus %s does not mention nucleus-catalogcmd", strings.Join(args, " "))
	}
	return absent
}

// EX-09 — the plugin reference points a plugin author at a runnable
// example, and the example it points at exists.
func probePluginSDKPointsAtExample(t *testing.T, _ *env) verdict {
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), "docs", "reference", "PLUGIN_SDK.md"))
	if err != nil {
		t.Logf("no plugin reference: %v", err)
		return absent
	}
	doc := string(raw)
	examples := findExamplePlugins(t)
	if strings.Contains(doc, "no runnable example plugin ships") {
		if len(examples) > 0 {
			t.Logf("the reference says no example ships, and the repository has %v: the sentence is stale", examples)
			return partial
		}
		t.Log("the reference says it in so many words: no runnable example plugin ships in-tree")
		return absent
	}
	for _, ex := range examples {
		if strings.Contains(doc, ex) {
			t.Logf("the reference points at %s", ex)
			return present
		}
	}
	t.Logf("the reference no longer says none ships, and points at none of %v", examples)
	return partial
}

// EX-10 — `nucleus plugin test --execute` exercises the contract: it sends
// a request envelope and reads the response. A plugin that advertises a
// capability and answers every request with garbage must fail it.
func probePluginTestExercisesEnvelope(t *testing.T, e *env) verdict {
	requireShell(t)
	dir := t.TempDir()
	writeExecutable(t, filepath.Join(dir, plugins.GenericBinaryPrefix+"catalogbroken"), `#!/bin/sh
if [ "$1" = "capabilities" ]; then
  echo '{"capabilities":["mail.send"]}'
  exit 0
fi
echo 'this is not an envelope'
exit 50
`)
	withPath(t, dir)
	r := e.cli("plugin", "test", "--provider", "catalogbroken", "--capability", plugins.CapabilityMailSend, "--execute", "--json")
	var report struct {
		Status  string `json:"status"`
		Details string `json:"details"`
	}
	if err := json.Unmarshal([]byte(r.stdout), &report); err != nil {
		t.Logf("plugin test answered no report: %s", r.all())
		return absent
	}
	t.Logf("plugin test --execute on a plugin that breaks every request: status %q, %q", report.Status, report.Details)
	switch report.Status {
	case "ok", "warning":
		return partial
	case "error":
		if strings.Contains(strings.ToLower(report.Details), "discovered") {
			return absent
		}
		return present
	}
	return absent
}

// EX-11 — a plugin author has an SDK side: a helper that reads the request
// envelope, calls the author's handler and writes the response — the shape
// of hashicorp/go-plugin's plugin.Serve — instead of re-implementing the
// envelope from the reference.
func probePluginAuthoringHelper(t *testing.T, _ *env) verdict {
	re := regexp.MustCompile(`(?m)^func (Serve|ServeStdio|ServePlugin|ServeCapability|Run|Handle|Main)\(`)
	if hits := sourceMatches(t, "pkg/plugins", re); len(hits) > 0 {
		t.Logf("an authoring helper: %v", hits)
		return present
	}
	if d := existingDirs(t, "pkg/plugins/sdk", "pkg/plugins/plugin", "pkg/pluginsdk", "pkg/plugins/serve"); len(d) > 0 {
		t.Logf("a plugin SDK package: %v", d)
		return partial
	}
	t.Log("pkg/plugins is the HOST side only (discover, probe, execute); no Serve/ServeStdio/Run/Handle for the plugin side, " +
		"and no pkg/plugins/sdk, pkg/pluginsdk or pkg/plugins/serve package")
	return absent
}

// EX-12 — an external plugin runs only when the configuration allows it.
// The reference's safety rules say the binary must be allowlisted; the
// probe puts one on PATH, configures nothing, and sees whether the runtime
// executes it anyway.
func probePluginAllowlist(t *testing.T, _ *env) verdict {
	requireShell(t)
	dir := t.TempDir()
	capture := filepath.Join(dir, "capture.jsonl")
	pluginScript(t, dir, "catalogallow", capture, plugins.CapabilityMailSend)
	withPath(t, dir)

	cfgFile := filepath.Join(t.TempDir(), "nucleus.yml")
	must(t, os.WriteFile(cfgFile, []byte("plugins:\n  allow_external: false\n  allowed: []\n"), 0o644))
	_, cfgErr := app.LoadConfig(cfgFile)
	t.Logf("a configuration with a plugins block: %v", errOrOK(cfgErr))

	sender, err := mail.NewSender(mail.Config{Driver: "catalogallow", Timeout: 5 * time.Second})
	if err != nil {
		t.Logf("the unlisted plugin was refused: %v", err)
		if cfgErr == nil {
			return present
		}
		return partial
	}
	_ = sender.Send(context.Background(), mail.Message{From: "bench@example.test", To: []string{"dev@example.test"}, Subject: "s", Body: "b"})
	if n := len(envelopes(t, capture)); n > 0 {
		t.Logf("a binary named nucleus-plugin-catalogallow, found on PATH and listed nowhere, received %d envelope(s)", n)
	}
	return absent
}

func errOrOK(err error) string {
	if err == nil {
		return "accepted"
	}
	return firstLines(err.Error(), 3)
}
