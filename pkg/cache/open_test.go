// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// This test binary does not import pkg/cache/rediscache, so "redis" is a
// name this project publishes that is NOT linked — the state an application
// is in when its configuration selects redis and nobody ran `nucleus add
// redis-cache`.

func TestOpen_NothingSelectedIsTheMemoryCache(t *testing.T) {
	for _, provider := range []string{"", "memory", " Memory "} {
		c, err := Open(t.Context(), Config{Provider: provider})
		if err != nil {
			t.Fatalf("Open(%q): %v", provider, err)
		}
		if _, ok := c.(*Memory); !ok {
			t.Fatalf("Open(%q) = %T, want *Memory", provider, c)
		}
	}
}

func TestOpen_SQLUsesTheTableTheCommandCreates(t *testing.T) {
	db := openSQLiteCacheTable(t)
	db.SetMaxOpenConns(1)
	c, err := Open(t.Context(), Config{Provider: "sql", DB: db, System: "sqlite"})
	if err != nil {
		t.Fatalf("Open(sql): %v", err)
	}
	if err := c.Set(t.Context(), "k", []byte("v"), time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got, ok, err := c.Get(t.Context(), "k"); err != nil || !ok || !bytes.Equal(got, []byte("v")) {
		t.Fatalf("Get = %q ok=%v err=%v", got, ok, err)
	}

	if _, err := Open(t.Context(), Config{Provider: "sql"}); err == nil || !strings.Contains(err.Error(), "needs a database") {
		t.Fatalf("Open(sql) without a database = %v, want a refusal that says it needs one", err)
	}
}

// A published backend the binary does not link is refused with the command
// that installs it and the import it stands for — never replaced by the
// memory cache, which would stop being shared without a word.
func TestOpen_RedisNotLinkedNamesTheCommand(t *testing.T) {
	_, err := Open(t.Context(), Config{Provider: "redis", URL: "redis://127.0.0.1:6379/0"})
	if err == nil {
		t.Fatal("Open(redis) without the backend linked succeeded")
	}
	for _, want := range []string{
		"nucleus add redis-cache",
		`import _ "github.com/jcsvwinston/nucleus/pkg/cache/rediscache"`,
		"not linked into this binary",
		"registered: memory, sql",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not say %q:\n%v", want, err)
		}
	}
}

func TestOpen_UnknownNameListsWhatIsRegistered(t *testing.T) {
	_, err := Open(t.Context(), Config{Provider: "memcache"})
	if err == nil || !strings.Contains(err.Error(), `unsupported provider "memcache"`) || !strings.Contains(err.Error(), "registered: memory, sql") {
		t.Fatalf("Open(memcache) = %v", err)
	}
	if strings.Contains(err.Error(), "nucleus add") {
		t.Fatalf("a name this project does not publish got an install hint: %v", err)
	}
}

func TestRegisterProvider_SelectsByNameWithThePrefixFilledIn(t *testing.T) {
	var got Config
	if err := RegisterProvider("open-test", func(_ context.Context, cfg Config) (Cache, error) {
		got = cfg
		return NewMemory(), nil
	}); err != nil {
		t.Fatalf("RegisterProvider: %v", err)
	}
	t.Cleanup(func() {
		registryMu.Lock()
		delete(registry, "open-test")
		registryMu.Unlock()
	})

	if _, err := Open(t.Context(), Config{Provider: "Open-Test", URL: "x://y"}); err != nil {
		t.Fatalf("Open(registered): %v", err)
	}
	if got.Provider != "open-test" || got.URL != "x://y" || got.Prefix != DefaultKeyPrefix {
		t.Fatalf("the factory received %+v", got)
	}
	names := strings.Join(RegisteredProviders(), ",")
	if names != "memory,open-test,sql" {
		t.Fatalf("RegisteredProviders() = %s", names)
	}
}

func TestRegisterProvider_RefusesBuiltInsDuplicatesAndBlanks(t *testing.T) {
	factory := func(context.Context, Config) (Cache, error) { return NewMemory(), nil }
	for name, f := range map[string]Factory{"memory": factory, "sql": factory, " ": factory, "nil-factory": nil} {
		if err := RegisterProvider(name, f); err == nil {
			t.Errorf("RegisterProvider(%q) was accepted", name)
		}
	}
	if err := RegisterProvider("dup-test", factory); err != nil {
		t.Fatalf("RegisterProvider: %v", err)
	}
	t.Cleanup(func() {
		registryMu.Lock()
		delete(registry, "dup-test")
		registryMu.Unlock()
	})
	if err := RegisterProvider("dup-test", factory); err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("registering a name twice = %v", err)
	}
	defer func() {
		if r := recover(); r == nil || !strings.Contains(fmt.Sprint(r), "already registered") {
			t.Errorf("MustRegisterProvider of a taken name recovered %v, want RegisterProvider's error", r)
		}
	}()
	MustRegisterProvider("dup-test", factory)
}
