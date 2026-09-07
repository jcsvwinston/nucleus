// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// defaultDevDebounce is how long the watcher waits after the last change
// before it rebuilds: an editor that saves through a temporary file and a
// rename, or a formatter that rewrites several files, produces a burst of
// events that must be one build, not one per event.
const defaultDevDebounce = 300 * time.Millisecond

// devStopGrace is how long a running application gets to shut down
// gracefully (SIGTERM: server drain, module OnShutdown) before the whole
// process group is killed so the port is free for the new binary.
const devStopGrace = 5 * time.Second

// defaultDevProxyPaths are the request path prefixes a --proxy front-end
// dev server answers instead of the application.
const defaultDevProxyPaths = "/static,/assets"

// devBuild is the build step of the loop, `go build -o <bin> .` in the
// directory of the main package. It is a variable so the tests (and the
// signal test's helper process) can stand in a builder that does not
// compile anything.
var devBuild = buildMainPackage

// devOptions is everything the loop needs, resolved by runDev from the
// flags; the tests fill it directly.
type devOptions struct {
	// dir is the directory of the main package, as the user passed it;
	// root is the module root it belongs to (the application's working
	// directory, where nucleus.yml and relative paths resolve).
	dir, root string
	// port is the port the application (or, with a proxy, the front) must
	// listen on; 0 lets the application read its own configuration, and
	// with a proxy falls back to the configured port.
	port int
	// proxy is the front-end dev server the proxyPaths are forwarded to;
	// nil means no front: the application listens on port itself.
	proxy      *url.URL
	proxyPaths []string
	// printRoutes runs the built binary with NUCLEUS_PRINT_ROUTES before
	// each start and prints the table it answers.
	printRoutes   bool
	routesTimeout time.Duration
	debounce      time.Duration
	// stdout carries the loop's own lines (prefixed "[dev]") and the
	// application's stdout; stderr the application's stderr.
	stdout, stderr io.Writer
}

// runDev is the development loop: it builds the main package in --dir,
// runs the binary with NUCLEUS_ENV=development (and NUCLEUS_PORT when
// --port is given), watches the project for changes to Go sources,
// nucleus.yml, rbac_policy.csv, migrations/ and templates/, and on each
// debounced change rebuilds; a successful build stops the running
// application and starts the new binary, a failed build prints the
// compiler's output and leaves the last good binary serving. --proxy puts
// the command in front: it listens on the port, forwards /static and
// /assets to a front-end dev server and everything else to the
// application, which then runs on a loopback port of its own. The
// application always runs in its own process group and is killed with it
// when the command ends, on Ctrl-C or SIGTERM as on a build that replaces
// it — nothing is left listening.
func runDev(args []string, _ io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("dev", flag.ContinueOnError)
	fs.SetOutput(stderr)
	installUsage(fs, "dev")

	dir := fs.String("dir", ".", "Directory of the application's main package; the project is the nearest go.mod at or above it")
	port := fs.Int("port", 0, "Port the application listens on (NUCLEUS_PORT); with --proxy, the port the front listens on. 0 keeps the configured port")
	proxy := fs.String("proxy", "", "URL of a front-end dev server that answers the --proxy-paths; the command then listens on the port in front of the application")
	proxyPaths := fs.String("proxy-paths", defaultDevProxyPaths, "Comma-separated request path prefixes forwarded to --proxy")
	printRoutes := fs.Bool("print-routes", false, "Print the routes of the binary (NUCLEUS_PRINT_ROUTES=1) before each start")
	debounce := fs.Duration("debounce", defaultDevDebounce, "How long to wait after the last change before rebuilding")
	routesTimeout := fs.Duration("routes-timeout", defaultRoutesTimeout, "How long the binary may take to print its routes with --print-routes")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if len(fs.Args()) > 0 {
		return usageError("dev")
	}
	if *port < 0 || *port > 65535 {
		return fmt.Errorf("invalid --port %d: expected 0-65535", *port)
	}
	if *debounce <= 0 {
		return fmt.Errorf("--debounce must be positive, got %s", *debounce)
	}
	if *routesTimeout <= 0 {
		return fmt.Errorf("--routes-timeout must be positive, got %s", *routesTimeout)
	}
	opts := devOptions{
		dir:           *dir,
		port:          *port,
		printRoutes:   *printRoutes,
		routesTimeout: *routesTimeout,
		debounce:      *debounce,
		stdout:        stdout,
		stderr:        stderr,
	}
	if *proxy != "" {
		u, err := parseDevProxyURL(*proxy)
		if err != nil {
			return err
		}
		opts.proxy = u
		opts.proxyPaths = splitDevProxyPaths(*proxyPaths)
		if len(opts.proxyPaths) == 0 {
			return fmt.Errorf("--proxy-paths must name at least one path prefix, got %q", *proxyPaths)
		}
	}

	root, inProject := findModuleRoot(*dir)
	if !inProject {
		return fmt.Errorf("no go.mod at or above %s: dev builds and runs the main package of a Go project; run it inside your project or pass --dir with the directory of your main package", *dir)
	}
	opts.root = root
	if err := ensureMainPackage(*dir, root); err != nil {
		return err
	}

	sigCtx, stop := signal.NotifyContext(context.Background(), routesStopSignals()...)
	defer stop()
	return devLoop(sigCtx, opts)
}

// parseDevProxyURL accepts the front-end dev server as an http(s) URL with
// a host; a bare "localhost:5173" is completed to http.
func parseDevProxyURL(raw string) (*url.URL, error) {
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid --proxy %q: %w", raw, err)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("invalid --proxy %q: expected an http or https URL with a host, like http://localhost:5173", raw)
	}
	return u, nil
}

// splitDevProxyPaths normalises the comma-separated prefixes: trimmed, with
// a leading slash and no trailing one, empty entries dropped.
func splitDevProxyPaths(list string) []string {
	var out []string
	for _, p := range strings.Split(list, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		p = "/" + strings.Trim(p, "/")
		if p == "/" {
			continue
		}
		out = append(out, p)
	}
	return out
}

// devBuildResult is what a build goroutine reports back to the loop.
type devBuildResult struct {
	generation int
	bin        string
	output     string
	err        error
}

// devLoop is the loop itself, ended by ctx. It returns nil when ctx ends:
// Ctrl-C is how a development session ends, not a failure.
func devLoop(ctx context.Context, opts devOptions) error {
	if opts.debounce <= 0 {
		opts.debounce = defaultDevDebounce
	}
	if opts.routesTimeout <= 0 {
		opts.routesTimeout = defaultRoutesTimeout
	}
	logf := func(format string, args ...any) {
		fmt.Fprintf(opts.stdout, "[dev] "+format+"\n", args...)
	}

	tmp, err := os.MkdirTemp("", "nucleus-dev-")
	if err != nil {
		return fmt.Errorf("create build directory: %w", err)
	}
	defer os.RemoveAll(tmp)

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("start the file watcher: %w", err)
	}
	defer watcher.Close()
	if err := watchTree(watcher, opts.root); err != nil {
		return fmt.Errorf("watch %s: %w", opts.root, err)
	}

	// The application's environment: development, and the port it must
	// take. Behind a proxy it takes a loopback port of its own and the
	// front takes the requested one.
	extraEnv := []string{"NUCLEUS_ENV=development"}
	appPort := opts.port
	if opts.proxy != nil {
		appPort, err = freeLoopbackPortForDev()
		if err != nil {
			return err
		}
		extraEnv = append(extraEnv, "NUCLEUS_HOST=127.0.0.1")
	}
	if appPort > 0 {
		extraEnv = append(extraEnv, "NUCLEUS_PORT="+strconv.Itoa(appPort))
	}
	env := append(os.Environ(), extraEnv...)
	if opts.proxy != nil {
		frontHost, frontPort := devFrontAddress(opts)
		ln, err := net.Listen("tcp", net.JoinHostPort(frontHost, strconv.Itoa(frontPort)))
		if err != nil {
			return fmt.Errorf("listen on %s:%d for the proxy: %w", frontHost, frontPort, err)
		}
		front := &http.Server{Handler: newDevProxy(opts.proxy, net.JoinHostPort("127.0.0.1", strconv.Itoa(appPort)), opts.proxyPaths)}
		go func() { _ = front.Serve(ln) }()
		defer front.Close()
		logf("proxy listening on http://%s: %s go to %s, everything else to the application on 127.0.0.1:%d", ln.Addr(), strings.Join(opts.proxyPaths, ", "), opts.proxy, appPort)
	}

	logf("watching %s for changes to Go sources, nucleus.yml, rbac_policy.csv, migrations/ and templates/", opts.root)

	var (
		child      *devChild
		childDone  chan error
		lastGood   string
		generation int
		building   bool
		dirty      bool
		built      = make(chan devBuildResult, 1)
		timer      = time.NewTimer(time.Hour)
	)
	timer.Stop()
	defer func() {
		timer.Stop()
		if child != nil {
			child.stop()
			logf("stopped the application")
		}
	}()

	startBuild := func() {
		generation++
		building = true
		dirty = false
		gen := generation
		bin := filepath.Join(tmp, fmt.Sprintf("app-%d", gen))
		logf("build #%d: go build . (in %s)", gen, opts.dir)
		go func() {
			out, err := devBuild(ctx, opts.dir, bin)
			built <- devBuildResult{generation: gen, bin: bin, output: out, err: err}
		}()
	}
	startBuild()

	for {
		select {
		case <-ctx.Done():
			return nil
		case res := <-built:
			building = false
			if ctx.Err() != nil {
				return nil
			}
			if res.err != nil {
				_ = os.Remove(res.bin)
				logf("build #%d failed:", res.generation)
				if res.output != "" {
					fmt.Fprintln(opts.stdout, res.output)
				} else {
					fmt.Fprintln(opts.stdout, res.err.Error())
				}
				if lastGood != "" {
					logf("keeping the last good binary running (build #%d); fix the error and save to rebuild", devGenerationOf(lastGood))
				} else {
					logf("no binary to run yet; fix the error and save to rebuild")
				}
			} else {
				logf("build #%d ok", res.generation)
				if opts.printRoutes {
					printDevRoutes(ctx, opts.stdout, logf, res.bin, opts, extraEnv)
				}
				if child != nil {
					child.stop()
					child, childDone = nil, nil
					logf("stopped the previous application")
				}
				if lastGood != "" {
					_ = os.Remove(lastGood)
				}
				lastGood = res.bin
				c, err := startDevChild(ctx, res.bin, opts.root, env, opts.stdout, opts.stderr)
				if err != nil {
					logf("start the application: %v; waiting for a change", err)
				} else {
					child, childDone = c, c.done
					where := "on its configured port"
					if appPort > 0 {
						where = fmt.Sprintf("on port %d", appPort)
					}
					logf("started build #%d (pid %d) %s", res.generation, c.cmd.Process.Pid, where)
				}
			}
			if dirty {
				timer.Reset(opts.debounce)
			}
		case ev, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			if ev.Has(fsnotify.Create) {
				if info, err := os.Stat(ev.Name); err == nil && info.IsDir() && !skipDevDir(filepath.Base(ev.Name)) {
					_ = watchTree(watcher, ev.Name)
				}
			}
			if !watchedDevFile(opts.root, ev.Name) {
				continue
			}
			if rel, err := filepath.Rel(opts.root, ev.Name); err == nil {
				logf("changed: %s", filepath.ToSlash(rel))
			}
			if building {
				dirty = true
				continue
			}
			timer.Reset(opts.debounce)
		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			logf("watch error: %v", err)
		case <-timer.C:
			if !building {
				startBuild()
			}
		case err := <-childDone:
			child, childDone = nil, nil
			if err != nil {
				logf("the application exited: %v; waiting for a change to rebuild", err)
			} else {
				logf("the application exited; waiting for a change to rebuild")
			}
		}
	}
}

// devGenerationOf reads the build number back from the binary name
// ("app-3" -> 3), so the "keeping" line names the build that is running.
func devGenerationOf(bin string) int {
	n, _ := strconv.Atoi(strings.TrimPrefix(filepath.Base(bin), "app-"))
	return n
}

// devFrontAddress is where the proxy listens: --port when given, else the
// port and host of the project's configuration (the address the
// application would have taken), else 127.0.0.1:8080.
func devFrontAddress(opts devOptions) (string, int) {
	host, port := "127.0.0.1", 8080
	if cfgPath := projectConfigPath(opts.dir, opts.root); cfgPath != "" {
		if cfg, err := loadConfig(cfgPath); err == nil {
			if cfg.Host != "" {
				host = cfg.Host
			}
			if cfg.Port > 0 {
				port = cfg.Port
			}
		}
	}
	if opts.port > 0 {
		port = opts.port
	}
	return host, port
}

// freeLoopbackPortForDev reserves and releases a loopback port for the
// application behind the proxy.
func freeLoopbackPortForDev() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("reserve a loopback port for the application: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port, nil
}

// newDevProxy is the front: requests under one of the prefixes go to the
// front-end dev server (WebSocket upgrades included, so hot reload works),
// everything else to the application. An application that is not
// answering — restarting, or failed to start — is a 503 that says so, not
// a connection refused in the browser.
func newDevProxy(front *url.URL, appAddr string, prefixes []string) http.Handler {
	assets := httputil.NewSingleHostReverseProxy(front)
	assets.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		http.Error(w, fmt.Sprintf("nucleus dev: the front-end dev server at %s is not answering: %v", front, err), http.StatusBadGateway)
	}
	application := httputil.NewSingleHostReverseProxy(&url.URL{Scheme: "http", Host: appAddr})
	application.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		http.Error(w, "nucleus dev: the application is not answering (restarting, or its last start failed — see the terminal); retry in a moment", http.StatusServiceUnavailable)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, p := range prefixes {
			if r.URL.Path == p || strings.HasPrefix(r.URL.Path, p+"/") {
				assets.ServeHTTP(w, r)
				return
			}
		}
		application.ServeHTTP(w, r)
	})
}

// printDevRoutes runs the freshly built binary with NUCLEUS_PRINT_ROUTES
// (the same environment the application is about to get) and prints the
// table, sorted by pattern; a binary that cannot print is reported and the
// start goes ahead — the listing is information, not a gate.
func printDevRoutes(ctx context.Context, w io.Writer, logf func(string, ...any), bin string, opts devOptions, extraEnv []string) {
	routes, err := readRouteDump(ctx, bin, opts.dir, opts.root, opts.routesTimeout, extraEnv)
	if err != nil {
		logf("routes: %v", err)
		return
	}
	sort.SliceStable(routes, func(i, j int) bool {
		if routes[i].Pattern == routes[j].Pattern {
			return routes[i].Method < routes[j].Method
		}
		return routes[i].Pattern < routes[j].Pattern
	})
	logf("routes: %d", len(routes))
	for _, r := range routes {
		owner := ""
		if r.Module != "" {
			owner = "  (" + r.Module + ")"
		}
		fmt.Fprintf(w, "[dev]   %-7s %s%s\n", r.Method, r.Pattern, owner)
	}
}

// devChild is the running application.
type devChild struct {
	cmd    *exec.Cmd
	cancel context.CancelFunc
	done   chan error
}

// startDevChild runs bin from root in its own process group, its output
// passed through to the command's own streams.
func startDevChild(parent context.Context, bin, root string, env []string, stdout, stderr io.Writer) (*devChild, error) {
	ctx, cancel := context.WithCancel(parent)
	cmd := exec.CommandContext(ctx, bin)
	cmd.Dir = root
	cmd.Env = env
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.WaitDelay = 2 * time.Second
	runInOwnProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	return &devChild{cmd: cmd, cancel: cancel, done: done}, nil
}

// stop asks the application to shut down gracefully (SIGTERM to its
// process group, which nucleus.Run turns into a server drain and the
// module OnShutdown hooks), and kills the whole group when it has not
// exited within devStopGrace. It returns once the process is gone, so the
// port is free for the next binary.
func (c *devChild) stop() {
	_ = terminateProcessGroup(c.cmd)
	select {
	case <-c.done:
	case <-time.After(devStopGrace):
		c.cancel()
		<-c.done
	}
	c.cancel()
}

// watchTree adds root and every directory below it to the watcher, skipping
// the trees that are never part of the build and would exhaust the
// watcher (.git and any other dot-directory, node_modules, vendor).
func watchTree(w *fsnotify.Watcher, root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == root {
				return err
			}
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if path != root && skipDevDir(d.Name()) {
			return filepath.SkipDir
		}
		if err := w.Add(path); err != nil && path == root {
			return err
		}
		return nil
	})
}

// skipDevDir names the directories the watcher never enters.
func skipDevDir(name string) bool {
	return strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor"
}

// watchedDevFile decides whether a change at path (inside root) is worth a
// rebuild: Go sources, the configuration file, the policy file, and
// anything under a migrations or templates directory. Editor artefacts
// (a dot-file, a backup ending in ~, a numbered swap file) are not.
func watchedDevFile(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return false
	}
	base := filepath.Base(rel)
	if strings.HasPrefix(base, ".") || strings.HasSuffix(base, "~") {
		return false
	}
	switch {
	case strings.HasSuffix(base, ".go"):
		return true
	case base == "nucleus.yml", base == "rbac_policy.csv":
		return true
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for _, dir := range parts[:len(parts)-1] {
		if dir == "migrations" || dir == "templates" {
			return true
		}
	}
	return false
}
