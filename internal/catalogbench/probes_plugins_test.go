// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package catalogbench

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
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
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/outbox"
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
	// Whatever an example writes by default — a mail provider's spool, a
	// cache — lands in a throwaway home, not the developer's.
	t.Setenv("HOME", t.TempDir())
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
// bridge would take. When the application accepts one, the probe enqueues a
// message into the running application's own outbox table — the way any
// writer of that table does, through pkg/outbox's store — and reads what
// the plugin receives and what the outbox records: the capability's
// envelope with the message in it, and the row delivered.
func probeOutboxPluginBridge(t *testing.T, e *env, capability string) verdict {
	requireShell(t)
	base := e.scaffold(t)
	dir := t.TempDir()
	capture := filepath.Join(dir, "capture.jsonl")
	pluginScript(t, dir, "catalogbridge", capture, capability)
	pathEnv := "PATH=" + dir + string(os.PathListSeparator) + os.Getenv("PATH")
	extra := ""
	if capability == plugins.CapabilityWebhookDeliver {
		extra = "        url: " + benchHookURL + "\n        secret: " + benchHookSecret + "\n"
	}
	for _, typ := range bridgeTypes {
		config := starterConfig(t, base, "outbox:\n  enabled: true\n  bridges:\n    - name: catalogbridge\n      type: "+typ+
			"\n      config:\n        provider: catalogbridge\n        capability: "+capability+"\n"+extra)
		runDir := t.TempDir()
		got, evidence := absent, ""
		b := bootIn(t, runDir, base.bin, config, []string{pathEnv}, func(_ int, output func() string) {
			if strings.Contains(output(), "unknown bridge type") {
				return
			}
			got, evidence = deliverThroughStarter(t, filepath.Join(runDir, "app.db"), capture, capability)
		})
		switch {
		case !b.listening:
			t.Logf("type %q: the starter refuses: %s", typ, firstLines(lastLines(b.output, 3), 3))
		case strings.Contains(b.output, "unknown bridge type"):
			t.Logf("type %q: WARN unknown bridge type — the application boots and the bridge is dropped", typ)
		default:
			t.Logf("type %q: %s", typ, evidence)
			if got == absent {
				return partial
			}
			return got
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

// The webhook a webhook.deliver bridge is configured with. Nothing listens
// there: the plugin is the transport, and the probe reads what it is handed.
const (
	benchHookURL    = "https://hooks.example.test/catalogbench"
	benchHookSecret = "catalogbench-secret"
)

// deliverThroughStarter enqueues one message into the running starter's
// outbox and waits for the plugin to receive it and the outbox to record
// it delivered. present: both, with the message in the envelope the
// capability defines; partial: the plugin received something, or the row
// moved, but not both or not the message.
func deliverThroughStarter(t *testing.T, dbPath, capture, capability string) (verdict, string) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=busy_timeout(10000)")
	if err != nil {
		return partial, "open the starter's database: " + err.Error()
	}
	defer func() { _ = db.Close() }()
	store, err := outbox.NewStore(db, outbox.Config{Flavor: outbox.FlavorSQLite})
	if err != nil {
		return partial, "the starter's outbox table: " + err.Error()
	}
	msg, err := store.Enqueue(context.Background(), outbox.Entry{Topic: "catalogbench.created", Payload: map[string]any{"hello": "world"}})
	if err != nil {
		return partial, "enqueue into the starter's outbox: " + err.Error()
	}
	deadline := time.Now().Add(20 * time.Second)
	var received *plugins.RequestEnvelope
	status := ""
	for time.Now().Before(deadline) {
		for _, env := range envelopes(t, capture) {
			if env.Metadata["outbox_message_id"] == msg.ID {
				env := env
				received = &env
			}
		}
		_ = db.QueryRow("SELECT status FROM nucleus_outbox WHERE id = ?", msg.ID).Scan(&status)
		if received != nil && status != string(outbox.StatusPending) && status != string(outbox.StatusProcessing) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if received == nil {
		return partial, fmt.Sprintf("the type is accepted, and the plugin received no envelope for message %s (outbox status %q)", msg.ID, status)
	}
	carries, what := envelopeCarries(*received, msg, capability)
	evidence := fmt.Sprintf("message %s reached the plugin as a %s envelope (%s); the outbox records it %q", msg.ID, received.Capability, what, status)
	if received.Capability != capability || !carries || status != string(outbox.StatusDelivered) {
		return partial, evidence
	}
	return present, evidence
}

// envelopeCarries checks the envelope holds the message the way the
// capability's schema says.
func envelopeCarries(env plugins.RequestEnvelope, msg outbox.Message, capability string) (bool, string) {
	switch capability {
	case plugins.CapabilityQueuePublish:
		var p plugins.QueuePublishPayload
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return false, "payload: " + err.Error()
		}
		ok := p.Topic == msg.Topic && p.Key == msg.ID && string(p.Body) == `{"hello":"world"}`
		return ok, fmt.Sprintf("topic %q, key %q, body %s", p.Topic, p.Key, p.Body)
	case plugins.CapabilityWebhookDeliver:
		var p plugins.WebhookDeliverPayload
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return false, "payload: " + err.Error()
		}
		signed := p.Headers[outbox.WebhookSignatureHeader] == nucleus.SignWebhookBody(benchHookSecret, []byte(p.Body))
		ok := p.URL == benchHookURL && strings.Contains(p.Body, `"id":"`+msg.ID+`"`) && signed
		return ok, fmt.Sprintf("url %s, the webhook body of the message: %t, signed over it: %t", p.URL, strings.Contains(p.Body, msg.ID), signed)
	}
	return false, "unknown capability"
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
var registryCall = regexp.MustCompile(`storage\.RegisterProvider\(|mail\.RegisterProvider\(|auth\.RegisterBackend\(|federated\.Register\(|exporter\.Register\(|secrets\.RegisterResolver\(|interceptor\.Register\(|\.RegisterBridge\(|nucleus\.Module\[|app\.Extension\b`)

// EX-05 — an in-process example — a provider registered through a public
// registry, or a module — ships as a fixture compiled and tested in CI. The
// repository keeps its examples as tested fixtures under internal/fixtures
// (owner decision: no examples/ directory); a directory named example or
// sample anywhere else counts too.
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
		rel, _ := filepath.Rel(root, path)
		slashed := filepath.ToSlash(rel)
		if !regexp.MustCompile(`(?i)example|sample`).MatchString(d.Name()) && !strings.HasPrefix(slashed, "internal/fixtures/") {
			return nil
		}
		files, _ := filepath.Glob(filepath.Join(path, "*.go"))
		hasTest, registers, command := false, false, false
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				hasTest = true
				continue
			}
			src, _ := os.ReadFile(f)
			if regexp.MustCompile(`(?m)^package main\b`).Match(src) {
				command = true // an external plugin: EX-01's, not an in-process example
			}
			if registryCall.Match(src) {
				registers = true
			}
		}
		if hasTest && registers && !command {
			found = append(found, slashed)
		}
		return nil
	})
	if len(found) == 0 {
		t.Log("no tested in-process example: nothing under internal/fixtures, and no directory named example/sample, holds a " +
			"tested package that registers a provider, a bridge, a module or an extension; the nearest things are the " +
			"first-party modules under providers/ and exporters/, which register the same way but are production code with " +
			"their own SDKs, not a starting point")
		return absent
	}
	for _, rel := range found {
		log, err := goRun(root, "test", "-count=1", "./"+rel)
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
// that test runs and passes outside any workspace, pinned to this checkout.
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
		conformance := testsCalling(modDir, "CheckModule")
		if log, err := goRun(modDir, "mod", "tidy"); err != nil {
			skipIfOffline(t, log)
			t.Logf("the template does not resolve: %s", log)
			return partial
		}
		log, err := goRun(modDir, "test", "-count=1", "-v", "./...")
		if err != nil {
			t.Logf("the template's tests fail standalone:\n%s", lastLines(log, 20))
			return partial
		}
		if len(conformance) == 0 {
			t.Log("the template's tests do not call nucleustest.CheckModule")
			return partial
		}
		for _, name := range conformance {
			if !strings.Contains(log, "--- PASS: "+name+" ") {
				t.Logf("%s calls CheckModule and did not run and pass:\n%s", name, lastLines(log, 20))
				return partial
			}
		}
		t.Logf("nucleus %s writes a standalone module; %v call nucleustest.CheckModule and pass outside any workspace", strings.Join(args, " "), conformance)
		return present
	}
	return absent
}

// testsCalling lists the test functions under dir whose body calls
// nucleustest.<fn>.
func testsCalling(dir, fn string) []string {
	var names []string
	fset := token.NewFileSet()
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return nil
		}
		for _, decl := range file.Decls {
			f, ok := decl.(*ast.FuncDecl)
			if !ok || f.Body == nil || !strings.HasPrefix(f.Name.Name, "Test") {
				continue
			}
			calls := false
			ast.Inspect(f.Body, func(n ast.Node) bool {
				if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == fn {
					if id, ok := sel.X.(*ast.Ident); ok && id.Name == "nucleustest" {
						calls = true
					}
				}
				return !calls
			})
			if calls {
				names = append(names, f.Name.Name)
			}
		}
		return nil
	})
	sort.Strings(names)
	return names
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
// The reference's safety rules say the binary must be allowlisted. The
// probe puts a mail plugin and an external command on PATH, loads each
// configuration through the strict check, builds the application from it
// and sends a message, and runs the CLI's dispatcher: the block must be
// accepted, an unlisted plugin refused before it is executed, a listed one
// run, and — the allowlist is opt-in until v2.0.0 (DEP-2026-014) — a
// configuration without the block must run the plugin as it always did.
func probePluginAllowlist(t *testing.T, e *env) verdict {
	requireShell(t)
	dir := t.TempDir()
	capture := filepath.Join(dir, "capture.jsonl")
	pluginScript(t, dir, "catalogallow", capture, plugins.CapabilityMailSend)
	commandScript(t, dir, "catalogallowcmd")
	withPath(t, dir)

	// send loads the configuration, builds the application from it and
	// sends one message; it answers how far that got and how many envelopes
	// the plugin has received by then.
	send := func(block string) (stage string, err error, received int) {
		cfgFile := filepath.Join(t.TempDir(), "nucleus.yml")
		must(t, os.WriteFile(cfgFile, []byte("mail_driver: catalogallow\n"+block), 0o644))
		loaded, err := app.LoadConfig(cfgFile)
		if err != nil {
			return "configuration", err, len(envelopes(t, capture))
		}
		cfg := inProcessConfig(t)
		cfg.Storage.Provider = "memory"
		cfg.MailDriver = loaded.MailDriver
		cfg.Plugins = loaded.Plugins
		a, err := app.New(&cfg)
		if err != nil {
			return "boot", err, len(envelopes(t, capture))
		}
		defer func() { _ = a.Shutdown(context.Background()) }()
		err = a.Mailer.Send(context.Background(), mail.Message{From: "bench@example.test", To: []string{"dev@example.test"}, Subject: "s", Body: "b"})
		return "send", err, len(envelopes(t, capture))
	}

	got := present
	for _, refused := range []struct{ name, block string }{
		{"allow_external: false", "plugins:\n  allow_external: false\n  allowed: []\n"},
		{"an allowlist without it", "plugins:\n  allowed:\n    - provider: someoneelse\n      capabilities: [mail.send]\n"},
	} {
		before := len(envelopes(t, capture))
		stage, err, received := send(refused.block)
		switch {
		case stage == "configuration":
			t.Logf("%s: the configuration is refused: %s", refused.name, firstLines(err.Error(), 3))
			got = absent
		case stage == "boot" && received == before:
			t.Logf("%s: the application refuses the plugin before running it: %s", refused.name, firstLines(err.Error(), 1))
		default:
			t.Logf("%s: the unlisted plugin ran (%s: %v; %d envelope(s))", refused.name, stage, err, received-before)
			got = absent
		}
	}
	if got == absent {
		return absent
	}

	for _, allowed := range []struct{ name, block string }{
		{"listed", "plugins:\n  allowed:\n    - provider: catalogallow\n      capabilities: [mail.send]\n"},
		{"no plugins block (opt-in default)", ""},
	} {
		before := len(envelopes(t, capture))
		if stage, err, received := send(allowed.block); err != nil || received != before+1 {
			t.Logf("%s: the plugin does not run (%s: %v; %d envelope(s))", allowed.name, stage, err, received-before)
			got = partial
		} else {
			t.Logf("%s: the plugin receives the envelope", allowed.name)
		}
	}

	dispatchCfg := filepath.Join(t.TempDir(), "nucleus.yml")
	t.Setenv("NUCLEUS_CONFIG", dispatchCfg)
	must(t, os.WriteFile(dispatchCfg, []byte("plugins:\n  commands: [someothercmd]\n"), 0o644))
	if r := e.cli("catalogallowcmd"); r.code == 3 {
		t.Log("nucleus catalogallowcmd runs although plugins.commands does not list it")
		got = partial
	} else {
		t.Logf("nucleus catalogallowcmd, unlisted: exit %d, %s", r.code, firstLines(r.stderr, 1))
	}
	must(t, os.WriteFile(dispatchCfg, []byte("plugins:\n  commands: [catalogallowcmd]\n"), 0o644))
	if r := e.cli("catalogallowcmd"); r.code != 3 {
		t.Logf("nucleus catalogallowcmd, listed: exit %d, %s", r.code, firstLines(r.all(), 2))
		got = partial
	}
	return got
}
