package contracts

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// modulePath is the Go module path; import paths for public packages are
// derived from it so each package row carries only its repo-relative path.
const modulePath = "github.com/jcsvwinston/nucleus"

// lifecycle is a package's posture per docs/reference/API_CONTRACT_INVENTORY.md.
// It is the governance anchor for whether a package is frozen: only `stable`
// packages are part of the API no-removal contract.
type lifecycle string

const (
	lifecycleStable        lifecycle = "stable"
	lifecycleTransitional  lifecycle = "transitional"
	lifecycleExperimental  lifecycle = "experimental"
	lifecycleUninventoried lifecycle = "uninventoried" // no inventory row yet
)

// publicPackage describes one top-level pkg/* package and which contract
// scanners apply to it. It is the single source of truth that both the API
// freeze (freeze_test.go) and the third-party firewall (firewall_test.go)
// derive their package sets from, replacing the two hand-maintained slices
// that previously drifted out of sync. The pkg/observability omission that
// went unnoticed until 2026-05-21 was a direct symptom of that duplication.
//
// Adding a directory under pkg/ WITHOUT adding a row here fails
// TestPublicPackages_RegistryMatchesFilesystem, so omissions surface as a
// red test rather than a silent gap discovered later.
type publicPackage struct {
	// relative is the repo-root-relative path, e.g. "pkg/app".
	relative string
	// lifecycle is the inventory posture (governance anchor for `frozen`).
	lifecycle lifecycle
	// frozen marks the package for the API no-removal freeze. True iff the
	// inventory marks it `stable` — see TestPublicPackages_FrozenMatchesLifecycle.
	frozen bool
	// firewalled marks the package for the third-party leak scan. True for
	// every public surface that imports (or could plausibly import) a
	// forbidden library. Pure-stdlib packages are intentionally false.
	firewalled bool
	// note documents a deliberate scanner exclusion so the reasoning lives
	// next to the data instead of in a prose block that rots. Empty for
	// packages that are both frozen and firewalled (the common case).
	note string
}

// importPath returns the package's fully-qualified Go import path.
func (p publicPackage) importPath() string {
	return modulePath + "/" + p.relative
}

// allPublicPackages is the authoritative registry of top-level pkg/* packages
// and the contract scanners that apply to each. Keep it sorted by relative
// path. The deliberate scanner exclusions are:
//
//   - pkg/circuit       — frozen but NOT firewalled: pure stdlib, has no
//     third-party dependency to leak.
//   - pkg/admin         — firewalled but NOT frozen: `transitional`; embedded
//     UI/handler details evolve faster than the runtime,
//     and it wraps go-redis so the firewall still guards it.
//   - pkg/outbox        — firewalled but NOT frozen: `transitional`; ergonomics
//     may tighten and NewKafkaBridge is deliberately
//     unfinished, so it must not be frozen until a real
//     Kafka delivery implementation lands.
//   - pkg/openapi       — neither: `experimental`; the helper surface may still
//     expand before v1.0.
//   - pkg/observability — neither: no inventory row yet (uninventoried); an
//     internal-facing, hot-path event bus backing the admin
//     observability agent, currently leak-free. Giving it
//     an inventory entry + a lifecycle decision is its own
//     tracked follow-up.
//
// Promoting any package to `stable` in the inventory is the trigger to flip
// `frozen` to true here and rebaseline with NUCLEUS_UPDATE_CONTRACT_BASELINE=1.
func allPublicPackages() []publicPackage {
	return []publicPackage{
		{relative: "pkg/admin", lifecycle: lifecycleTransitional, frozen: false, firewalled: true, note: "transitional: UI/handler details evolve faster than the runtime; wraps go-redis so the firewall still guards it"},
		{relative: "pkg/app", lifecycle: lifecycleStable, frozen: true, firewalled: true},
		{relative: "pkg/auth", lifecycle: lifecycleStable, frozen: true, firewalled: true},
		{relative: "pkg/authz", lifecycle: lifecycleStable, frozen: true, firewalled: true},
		{relative: "pkg/circuit", lifecycle: lifecycleStable, frozen: true, firewalled: false, note: "pure stdlib: no third-party dependency to leak"},
		{relative: "pkg/db", lifecycle: lifecycleStable, frozen: true, firewalled: true},
		{relative: "pkg/errors", lifecycle: lifecycleStable, frozen: true, firewalled: true},
		{relative: "pkg/health", lifecycle: lifecycleStable, frozen: true, firewalled: true},
		{relative: "pkg/mail", lifecycle: lifecycleStable, frozen: true, firewalled: true},
		{relative: "pkg/model", lifecycle: lifecycleStable, frozen: true, firewalled: true},
		{relative: "pkg/nucleus", lifecycle: lifecycleStable, frozen: true, firewalled: true},
		{relative: "pkg/observability", lifecycle: lifecycleUninventoried, frozen: false, firewalled: false, note: "no inventory row yet: internal-facing hot-path event bus, currently leak-free; needs a lifecycle decision (tracked follow-up)"},
		{relative: "pkg/observe", lifecycle: lifecycleStable, frozen: true, firewalled: true},
		{relative: "pkg/openapi", lifecycle: lifecycleExperimental, frozen: false, firewalled: false, note: "experimental: helper surface may still expand before v1.0"},
		{relative: "pkg/outbox", lifecycle: lifecycleTransitional, frozen: false, firewalled: true, note: "transitional: NewKafkaBridge is deliberately unfinished; must not be frozen until real Kafka delivery lands"},
		{relative: "pkg/plugins", lifecycle: lifecycleStable, frozen: true, firewalled: true},
		{relative: "pkg/router", lifecycle: lifecycleStable, frozen: true, firewalled: true},
		{relative: "pkg/signals", lifecycle: lifecycleStable, frozen: true, firewalled: true},
		{relative: "pkg/storage", lifecycle: lifecycleStable, frozen: true, firewalled: true},
		{relative: "pkg/tasks", lifecycle: lifecycleStable, frozen: true, firewalled: true},
		{relative: "pkg/validate", lifecycle: lifecycleStable, frozen: true, firewalled: true},
	}
}

// frozenPackages returns the registry subset covered by the API freeze.
func frozenPackages() []publicPackage {
	all := allPublicPackages()
	out := make([]publicPackage, 0, len(all))
	for _, p := range all {
		if p.frozen {
			out = append(out, p)
		}
	}
	return out
}

// firewalledPackages returns the registry subset covered by the leak scan.
func firewalledPackages() []publicPackage {
	all := allPublicPackages()
	out := make([]publicPackage, 0, len(all))
	for _, p := range all {
		if p.firewalled {
			out = append(out, p)
		}
	}
	return out
}

// TestPublicPackages_RegistryMatchesFilesystem fails if a top-level pkg/*
// directory containing Go source is missing from allPublicPackages() (a
// silent omission) or if the registry references a directory that no longer
// exists (a stale row). This is what makes scanner coverage gaps machine-
// visible. Nested packages (e.g. pkg/tasks/providers/*) are intentionally
// out of scope: the freeze and firewall scanners operate per top-level
// directory only.
func TestPublicPackages_RegistryMatchesFilesystem(t *testing.T) {
	repoRoot := filepath.Dir(baselinePath(t))

	onDisk := discoverTopLevelPublicPackages(t, repoRoot)
	inRegistry := make(map[string]struct{}, len(allPublicPackages()))
	for _, p := range allPublicPackages() {
		inRegistry[p.relative] = struct{}{}
	}

	var missing, stale []string
	for rel := range onDisk {
		if _, ok := inRegistry[rel]; !ok {
			missing = append(missing, rel)
		}
	}
	for rel := range inRegistry {
		if _, ok := onDisk[rel]; !ok {
			stale = append(stale, rel)
		}
	}
	sort.Strings(missing)
	sort.Strings(stale)

	if len(missing) > 0 {
		t.Errorf("public packages on disk but missing from allPublicPackages(): %v\n"+
			"add a row to contracts/packages_test.go and decide frozen/firewalled posture", missing)
	}
	if len(stale) > 0 {
		t.Errorf("allPublicPackages() references directories that no longer exist: %v\n"+
			"remove the stale row(s) from contracts/packages_test.go", stale)
	}
}

// TestPublicPackages_FrozenMatchesLifecycle enforces the invariant that a
// package is frozen iff its inventory lifecycle is `stable`. This catches a
// row that marks a transitional/experimental package frozen (or forgets to
// freeze a newly-promoted stable one).
func TestPublicPackages_FrozenMatchesLifecycle(t *testing.T) {
	for _, p := range allPublicPackages() {
		wantFrozen := p.lifecycle == lifecycleStable
		if p.frozen != wantFrozen {
			t.Errorf("%s: frozen=%v but lifecycle=%q (frozen must be true iff lifecycle is %q)",
				p.relative, p.frozen, p.lifecycle, lifecycleStable)
		}
	}
}

// discoverTopLevelPublicPackages returns the set of pkg/<name> directories
// that contain at least one non-test .go file directly (non-recursive),
// keyed by repo-relative path. Build constraints are not evaluated: any
// non-test .go file name is sufficient, so a directory holding only
// build-tagged-out source (e.g. //go:build ignore) still counts.
func discoverTopLevelPublicPackages(t *testing.T, repoRoot string) map[string]struct{} {
	t.Helper()
	pkgRoot := filepath.Join(repoRoot, "pkg")
	entries, err := os.ReadDir(pkgRoot)
	if err != nil {
		t.Fatalf("read pkg dir %s: %v", pkgRoot, err)
	}

	out := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(pkgRoot, entry.Name())
		if !hasGoSource(t, dir) {
			continue
		}
		out["pkg/"+entry.Name()] = struct{}{}
	}
	return out
}

// hasGoSource reports whether dir contains at least one non-test .go file.
func hasGoSource(t *testing.T, dir string) bool {
	t.Helper()
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
	}
	for _, f := range files {
		if f.IsDir() {
			continue
		}
		name := f.Name()
		if strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") {
			return true
		}
	}
	return false
}
