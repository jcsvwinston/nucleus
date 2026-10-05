// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package catalogbench

import (
	"bufio"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// EN-02 measures `nucleus add saml` on the starter with a sign-in end to end
// against a stand-in SAML identity provider. The bench cannot sign SAML
// itself: XML signatures need a library this module does not depend on, and
// writing one here would measure the bench. So the stand-in is the SAML
// module's own test identity provider (providers/auth-saml/samltest, built
// on the library the provider verifies with), compiled as a program inside
// the project `nucleus add saml` left — whose module graph already holds
// that library — and run beside the application.

// samlPlaceholderMetadata is the identity provider the saml recipe writes;
// the person replaces it with theirs, and so does the probe.
const samlPlaceholderMetadata = "idp_metadata_url: https://idp.example.com/saml/metadata"

// samlConfig selects the saml provider without the module, for CAT-01.
const samlConfig = "public_base_url: http://127.0.0.1:8080\nauth_federated:\n  - name: corp\n    provider: saml\n" +
	"auth:\n  corp:\n    sp_entity_id: http://127.0.0.1:8080/auth/corp/metadata\n    idp_metadata_url: http://127.0.0.1:1/metadata\n"

// samlBenchIdP is the program the probe adds to the project: it starts the
// stand-in identity provider, prints its metadata URL and serves until it
// is stopped.
const samlBenchIdP = `package main

import (
	"fmt"
	"os"
	"os/signal"

	"github.com/jcsvwinston/nucleus/providers/auth-saml/samltest"
)

func main() {
	idp, err := samltest.New()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	idp.User = samltest.User{
		NameID:     "bench-subject",
		Attributes: map[string][]string{"uid": {"bench-user"}, "mail": {"bench@example.test"}},
	}
	idp.Start()
	defer idp.Close()
	fmt.Println(idp.MetadataURL())
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	<-stop
}
`

// startSAMLBenchIdP writes the program into the project, builds it with the
// project's module graph read-only, runs it and returns its metadata URL.
func startSAMLBenchIdP(t *testing.T, dir string) string {
	t.Helper()
	src := filepath.Join(dir, "benchidp")
	must(t, os.MkdirAll(src, 0o755))
	must(t, os.WriteFile(filepath.Join(src, "main.go"), []byte(samlBenchIdP), 0o644))
	bin := filepath.Join(t.TempDir(), exeName("benchidp"))
	if log, err := goRun(dir, "build", "-o", bin, "./benchidp"); err != nil {
		skipIfOffline(t, log)
		t.Fatalf("the stand-in identity provider does not build in the project nucleus add saml left:\n%s", log)
	}
	cmd := exec.Command(bin)
	out, err := cmd.StdoutPipe()
	must(t, err)
	cmd.Stderr = os.Stderr
	must(t, cmd.Start())
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	line := make(chan string, 1)
	go func() {
		s := bufio.NewScanner(out)
		if s.Scan() {
			line <- strings.TrimSpace(s.Text())
		}
		_, _ = io.Copy(io.Discard, out)
	}()
	select {
	case u := <-line:
		return u
	case <-time.After(30 * time.Second):
		t.Fatal("the stand-in identity provider printed no metadata URL")
		return ""
	}
}

var (
	samlFormAction = regexp.MustCompile(`<form method="post" action="([^"]*)"`)
	samlFormField  = func(name string) *regexp.Regexp {
		return regexp.MustCompile(`name="` + name + `" value="([^"]*)"`)
	}
)

// samlSignIn is EN-02's wiring check, what a browser does: the metadata the
// identity provider is configured from is served; the start route sends the
// browser to the identity provider with an AuthnRequest; the identity
// provider answers with the auto-posting form; the form posted to the
// callback signs in (the Response verified — the assertion's signature,
// audience, recipient, InResponseTo, validity window); and the same form
// posted again does not.
func samlSignIn(idpMetadata func() string) func(t *testing.T, e *env, run coreRun) (bool, string) {
	return func(t *testing.T, _ *env, run coreRun) (bool, string) {
		jar, _ := cookiejar.New(nil)
		browser := &http.Client{Jar: jar, Timeout: 10 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		base := fmt.Sprintf("http://127.0.0.1:%d", run.port)
		idpBase := strings.TrimSuffix(idpMetadata(), "/metadata")

		resp, err := browser.Get(base + "/auth/corp/metadata")
		if err != nil {
			return false, err.Error()
		}
		md, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK || !strings.Contains(resp.Header.Get("Content-Type"), "samlmetadata") ||
			!strings.Contains(string(md), "/auth/corp/callback") {
			return false, fmt.Sprintf("GET /auth/corp/metadata answered %d %q: %s", resp.StatusCode, resp.Header.Get("Content-Type"), firstLines(string(md), 3))
		}

		resp, err = browser.Get(base + "/auth/corp/start")
		if err != nil {
			return false, err.Error()
		}
		_ = resp.Body.Close()
		location := resp.Header.Get("Location")
		if resp.StatusCode != http.StatusFound || !strings.HasPrefix(location, idpBase+"/sso?") || !strings.Contains(location, "SAMLRequest=") {
			return false, fmt.Sprintf("GET /auth/corp/start answered %d to %q, not an AuthnRequest to the identity provider", resp.StatusCode, location)
		}

		resp, err = browser.Get(location)
		if err != nil {
			return false, err.Error()
		}
		page, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		action := samlFormAction.FindStringSubmatch(string(page))
		response := samlFormField("SAMLResponse").FindStringSubmatch(string(page))
		relay := samlFormField("RelayState").FindStringSubmatch(string(page))
		if resp.StatusCode != http.StatusOK || action == nil || response == nil || relay == nil {
			return false, fmt.Sprintf("the identity provider answered %d without the form a browser posts: %s", resp.StatusCode, firstLines(string(page), 3))
		}
		acs := html.UnescapeString(action[1])
		form := url.Values{"SAMLResponse": {html.UnescapeString(response[1])}, "RelayState": {html.UnescapeString(relay[1])}}

		resp, err = browser.PostForm(base+"/auth/corp/callback", form)
		if err != nil {
			return false, err.Error()
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `"username":"bench-user"`) || !strings.Contains(string(body), `"id":"bench-subject"`) {
			return false, fmt.Sprintf("POST /auth/corp/callback answered %d: %s", resp.StatusCode, firstLines(string(body), 2))
		}

		resp, err = browser.PostForm(base+"/auth/corp/callback", form)
		if err != nil {
			return false, err.Error()
		}
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return false, "the same SAMLResponse posted a second time signed in again"
		}
		return true, fmt.Sprintf("GET /auth/corp/metadata served the SP metadata; /auth/corp/start sent an AuthnRequest to the identity provider (ACS %s); "+
			"the signed Response posted to the callback answered 200 for bench-user, and posted again answered %d", acs, resp.StatusCode)
	}
}

func probeEntrySAML(t *testing.T, e *env) verdict {
	if r := e.dryRun("saml"); r.code != 0 {
		t.Logf("nucleus add saml: %s", firstLines(r.stderr, 1))
		return probeMissingEntry(t, e, missingEntry{
			names: []string{"saml", "saml2", "auth-saml"},
			dirs:  []string{"pkg/auth/federated/saml", "providers/saml", "providers/auth-saml", "providers/federated-saml"},
			deps:  []string{"github.com/crewjam/saml", "github.com/russellhaering/gosaml2"},
			src:   regexp.MustCompile(`federated\.Register\("saml"|federated\.Register\(ProviderName`),
			under: ".",
		})
	}
	var metadataURL string
	c := coreEntry{
		name: "saml",
		// The identity provider has to be up before the application boots
		// with its address, and it is built from the project the command
		// left — so it starts here, where the project is.
		code: func(t *testing.T, dir string) { metadataURL = startSAMLBenchIdP(t, dir) },
		// What the person does after `nucleus add saml`: point the metadata
		// URL at their identity provider.
		edit: func(t *testing.T, _ *env, config string) string {
			if !strings.Contains(config, samlPlaceholderMetadata) {
				t.Logf("nucleus.yml carries no %q to replace; booting it as written", samlPlaceholderMetadata)
				return config
			}
			return strings.Replace(config, samlPlaceholderMetadata, "idp_metadata_url: "+metadataURL, 1)
		},
		check: samlSignIn(func() string { return metadataURL }),
	}
	return addedEntryVerdict(t, e, c)
}
