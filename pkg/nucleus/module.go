package nucleus

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"time"

	"github.com/knadh/koanf/v2"
)

// Middleware is a standard net/http middleware function. The framework's
// router applies middleware in registration order; the user-facing
// builder method `AppBuilder.Use(...)` and the per-module `Module[C].Middleware`
// field both accept values of this type.
type Middleware = func(http.Handler) http.Handler

// JobSpec describes one background job a module registers through
// JobRegistry.Register. Exactly one of Every or Cron must be set.
type JobSpec struct {
	// Handler runs on every scheduled tick. Required. It receives a
	// context that is cancelled on application shutdown (and bounded by
	// Timeout when one is set); a non-nil error is logged, never fatal.
	Handler func(ctx context.Context) error

	// Every schedules the job at a fixed interval (e.g. 30*time.Second).
	// Mutually exclusive with Cron.
	Every time.Duration

	// Cron schedules the job with a standard 5-field cron expression
	// ("*/5 * * * *") or a descriptor ("@hourly", "@daily", "@every 90s").
	// The framework validates the expression at registration time —
	// boot fails on an invalid spec — and translates it for the
	// configured provider, so the same spec means the same schedule on
	// both the memory and the asynq provider. Mutually exclusive with
	// Every.
	Cron string

	// Timeout bounds each run with a context deadline. Zero means no
	// per-run deadline; the run still stops when the application shuts
	// down.
	Timeout time.Duration

	// Singleton skips a tick while the previous run of this job is
	// still executing in this process, instead of overlapping runs.
	// Each skip is logged at WARN.
	Singleton bool
}

// JobRegistry is the surface a module's Jobs closure receives to
// register background jobs. Jobs are executed by the provider selected
// with the `jobs_provider` config key: "memory" (default, in-process,
// backed by pkg/tasks/providers/memory) or "asynq" (Redis-backed,
// `jobs_redis_url` required). Registration errors — empty name, nil
// handler, missing or ambiguous schedule, invalid cron expression,
// duplicate name within a module — fail application boot.
type JobRegistry interface {
	Register(name string, spec JobSpec) error
}

// WebhookSpec describes one inbound webhook a module registers through
// WebhookRegistry.Register.
type WebhookSpec struct {
	// Handler serves the request after the framework's checks (method,
	// body cap, signature) pass. Required. The request body has already
	// been read for signature verification and is replayed, so Handler
	// reads it as usual.
	Handler http.HandlerFunc

	// Secret, when non-empty, requires each request to carry a valid
	// HMAC-SHA256 signature of the raw request body in the
	// X-Nucleus-Signature header, formatted "sha256=<hex>" (see
	// SignWebhookBody). Requests with a missing or invalid signature
	// are rejected with 401 before Handler runs. When empty, no
	// signature is checked and the framework WARNs once at boot —
	// Handler must then authenticate the caller itself.
	//
	// Declared limit — no anti-replay: the signature authenticates
	// content, not freshness or uniqueness. A captured signed request
	// verifies again if re-sent verbatim. Handlers whose effects are not
	// idempotent should deduplicate by an event ID carried in the
	// payload; setting TimestampTolerance additionally narrows the
	// replay window to that tolerance (dedup is still what closes it
	// within the window).
	Secret string

	// Methods restricts the accepted HTTP methods. Default: POST only.
	// Other methods receive 405 with an Allow header.
	Methods []string

	// MaxBytes caps the request body size; larger bodies are rejected
	// with 413. Default: 1 MiB.
	MaxBytes int64

	// TimestampTolerance, when positive, switches the signature check to
	// the timestamped scheme: each request must carry its send time as
	// decimal Unix seconds in the X-Nucleus-Timestamp header, the time
	// must lie within ±TimestampTolerance of the receiver's clock, and
	// the signature must cover `<timestamp>.<body>` — senders use
	// SignWebhookBodyWithTimestamp, which returns both header values.
	// Missing, malformed, out-of-tolerance, or unsigned timestamps are
	// rejected with 401 before Handler runs.
	//
	// Zero (the default) keeps the compatible body-only scheme of
	// SignWebhookBody with no timestamp requirement. Opt-in because it
	// changes what senders must sign; 5m is a reasonable tolerance for
	// senders with NTP-synced clocks. Setting it without a Secret is a
	// registration error (the timestamp is only trustworthy signed), as
	// is a negative value.
	TimestampTolerance time.Duration
}

// WebhookRegistry is the surface a module's Webhooks closure receives
// to register inbound webhook handlers. Each registration mounts a real
// route at `<webhooks_prefix>/<module-name><path>` (webhooks_prefix
// defaults to "/webhooks") on the application router. When CSRF
// protection is enabled, the webhook prefix is exempted automatically —
// webhooks authenticate by signature, not by CSRF token. Registration
// errors — empty path, non-canonical path (one that path.Clean would
// rewrite: "." or ".." segments, duplicate or trailing slashes), nil
// handler, duplicate path, or a TimestampTolerance that is negative or
// lacks a Secret — fail application boot.
type WebhookRegistry interface {
	Register(path string, spec WebhookSpec) error
}

// ModuleSpec is the type-erased interface every module satisfies. It is
// the shape stored in `App.Modules` and consumed by `AppBuilder.Mount`
// and the framework's startup sequence.
//
// Modules are self-contained units of feature organisation: a module
// brings its own routes, models, migrations, jobs and webhooks, and
// can be lifted into another application by adding it to that
// application's `Mount(...)` list.
//
// Users do not implement `ModuleSpec` directly. They construct a
// `Module[C any]` with a typed configuration and call its `Build()`
// method, which returns a `ModuleSpec` wrapper.
type ModuleSpec interface {
	Name() string
	Prefix() string
	DefaultDB() string
	Requires() []string
	Models() []any
	Middleware() []Middleware
	Routes(r Router)
	Jobs(j JobRegistry)
	Webhooks(w WebhookRegistry)
	Migrations() fs.FS
	// OnStart runs before the module's Routes are registered (ADR-010
	// Phase 4, Gap 2), so a module initialises its managed resources here
	// — typically `rt.DB()` — and its Routes closure can then capture that
	// state directly. The `Runtime` handle replaces the former `*App`
	// config struct so modules reach the framework-managed connection pool
	// instead of opening their own.
	OnStart(ctx context.Context, rt Runtime) error
	OnShutdown(ctx context.Context, rt Runtime) error
	Config() any
}

// Module is the generic constructor for typed module configs. Users
// instantiate it with their config type. The framework binds
// `modules.<Name>.*` into the `Config` field during configuration load
// (Phase 2 — the validator landing point); Phase 1 establishes the
// shape so module authors can adopt the generic surface today.
//
// Call `Build()` to obtain the type-erased `ModuleSpec` that
// `AppBuilder.Mount` and `nucleus.App.Modules` expect.
type Module[C any] struct {
	Name       string
	Prefix     string
	DefaultDB  string
	Requires   []string
	Models     []any
	Middleware []Middleware
	// Config is the module's typed configuration. At Run time (ADR-010 §2
	// layer 5) the framework binds the `modules.<Name>.*` config subtree into
	// it, fills still-zero fields from `default:` struct tags, and validates it
	// against `validate:` tags. Precedence is: a value set here (the
	// programmatic baseline) < the config file < `default:` tags fill only what
	// remains zero. Because defaulting keys off the zero value, a field
	// deliberately left at its zero value cannot be distinguished from "unset"
	// and will receive its `default:` tag value if it has one.
	Config C
	Routes func(r Router, cfg C)
	// Jobs registers background jobs on the real registry (executed by
	// the provider selected with `jobs_provider`); Webhooks mounts
	// signed inbound webhook routes under `webhooks_prefix`. Both run
	// once at startup, after OnStart. See JobRegistry / WebhookRegistry.
	Jobs       func(j JobRegistry, cfg C)
	Webhooks   func(w WebhookRegistry, cfg C)
	Migrations fs.FS
	OnStart    func(ctx context.Context, rt Runtime, cfg C) error
	OnShutdown func(ctx context.Context, rt Runtime, cfg C) error
}

// Build returns the type-erased `ModuleSpec` for this `Module[C]`,
// suitable for storage in `App.Modules` and `AppBuilder.Mount(...)`.
// The returned spec captures the module's typed `Config` by value so
// modifications to the Module after Build do not leak into the spec.
func (m Module[C]) Build() ModuleSpec {
	return moduleSpec[C]{m: m}
}

// moduleSpec is the unexported type-erased wrapper produced by
// `Module[C].Build()`. Function callbacks are invoked with the
// captured typed config so module authors keep compile-time type
// safety; only the framework's internal storage works through the
// `ModuleSpec` interface.
type moduleSpec[C any] struct {
	m Module[C]
}

func (s moduleSpec[C]) Name() string       { return s.m.Name }
func (s moduleSpec[C]) Prefix() string     { return s.m.Prefix }
func (s moduleSpec[C]) DefaultDB() string  { return s.m.DefaultDB }
func (s moduleSpec[C]) Requires() []string { return s.m.Requires }
func (s moduleSpec[C]) Models() []any      { return s.m.Models }
func (s moduleSpec[C]) Middleware() []Middleware {
	return s.m.Middleware
}
func (s moduleSpec[C]) Routes(r Router) {
	if s.m.Routes == nil {
		return
	}
	s.m.Routes(r, s.m.Config)
}
func (s moduleSpec[C]) Jobs(j JobRegistry) {
	if s.m.Jobs == nil {
		return
	}
	s.m.Jobs(j, s.m.Config)
}
func (s moduleSpec[C]) Webhooks(w WebhookRegistry) {
	if s.m.Webhooks == nil {
		return
	}
	s.m.Webhooks(w, s.m.Config)
}
func (s moduleSpec[C]) Migrations() fs.FS { return s.m.Migrations }

// hasJobs reports whether the module declared a Jobs closure. It lets the
// startup sequence decide whether to build the jobs runtime at all without
// invoking the closure, and is intentionally unexported and kept off the
// public ModuleSpec contract.
func (s moduleSpec[C]) hasJobs() bool { return s.m.Jobs != nil }

// hasWebhooks reports whether the module declared a Webhooks closure. It
// feeds the automatic CSRF exemption of the webhook prefix, which must be
// decided before app.New builds the middleware stack — i.e. before any
// Webhooks closure can run.
func (s moduleSpec[C]) hasWebhooks() bool { return s.m.Webhooks != nil }

func (s moduleSpec[C]) OnStart(ctx context.Context, rt Runtime) error {
	if s.m.OnStart == nil {
		return nil
	}
	return s.m.OnStart(ctx, rt, s.m.Config)
}
func (s moduleSpec[C]) OnShutdown(ctx context.Context, rt Runtime) error {
	if s.m.OnShutdown == nil {
		return nil
	}
	return s.m.OnShutdown(ctx, rt, s.m.Config)
}
func (s moduleSpec[C]) Config() any { return s.m.Config }

// moduleConfigBinder is the unexported capability the framework type-asserts on
// a ModuleSpec to bind its typed config at Run time (ADR-010 §2 layer 5). Only
// the framework's own moduleSpec[C] wrapper implements it — users construct
// modules via Module[C].Build(), never by implementing ModuleSpec directly — so
// bindModuleConfigs can fall back gracefully for any foreign implementation.
type moduleConfigBinder interface {
	bindConfig(raw *koanf.Koanf) (ModuleSpec, error)
}

// moduleIntrospector is the unexported predicate view the framework
// type-asserts on a ModuleSpec to make pre-invocation decisions: whether any
// module declares webhooks (drives the automatic CSRF exemption of the
// webhook prefix, decided before app.New) and whether the jobs runtime needs
// to be built at all. Like moduleConfigBinder, only the framework's own
// moduleSpec[C] wrapper implements it, so the assertion degrades gracefully —
// a foreign ModuleSpec simply gets no automatic exemption. Kept off the
// public ModuleSpec contract so this stays an internal detail.
type moduleIntrospector interface {
	hasJobs() bool
	hasWebhooks() bool
}

// bindConfig produces a new ModuleSpec whose typed Config has been bound for
// ADR-010 §2 layer 5. Starting from the author-supplied Config (the
// programmatic baseline), it overlays the module's `modules.<name>.*` file
// subtree (when present), fills still-zero fields from `default:` struct tags,
// then validates the result against its `validate:` tags. The Config is a value
// field, so binding returns a fresh moduleSpec[C] rather than mutating in place;
// the caller swaps it into App.Modules before Routes/OnStart run.
//
// A nil raw (the direct-struct Run surface, where there is no config file) skips
// the file overlay but still applies defaults and validation, mirroring how
// layers 3 and 4 run on both the FromConfigFile and direct-struct paths.
func (s moduleSpec[C]) bindConfig(raw *koanf.Koanf) (ModuleSpec, error) {
	cfg := s.m.Config
	if raw != nil {
		if err := raw.Unmarshal("", &cfg); err != nil {
			return nil, fmt.Errorf("%w: module %q: binding modules.%s.*: %w", ErrInvalidModuleConfig, s.m.Name, s.m.Name, err)
		}
	}
	if err := applyDefaults(&cfg); err != nil {
		return nil, fmt.Errorf("%w: module %q: applying defaults: %w", ErrInvalidModuleConfig, s.m.Name, err)
	}
	if err := validateModuleConfigValue(s.m.Name, cfg); err != nil {
		return nil, err
	}
	bound := s.m
	bound.Config = cfg
	return moduleSpec[C]{m: bound}, nil
}
