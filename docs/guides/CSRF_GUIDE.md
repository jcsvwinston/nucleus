# CSRF Protection Guide

Nucleus provides enhanced CSRF protection with a Laravel-style two-layer approach:
1. **Origin verification** via `Sec-Fetch-Site` header (modern browsers)
2. **Traditional token validation** as fallback

## Quick Start

Default configuration (recommended for most apps):

```go
import "github.com/jcsvwinston/nucleus/pkg/router"

// CSRF is enabled by default with origin verification
r := router.New(logger,
    router.WithCSRF("/api/public", "/webhook/stripe"),
)
```

## Configuration Options by Use Case

### 1. Traditional Web Apps (Server-Side Rendering)

**Use:** Session-based token storage for enhanced security

```go
r := router.New(logger,
    router.WithCSRF(),
)

// In your app initialization, ensure session manager is set
mux.SetSessionManager(sessionManager)
```

**Why:** Tokens stored in session are not accessible by JavaScript, providing better security than cookies.

---

### 2. Single Page Applications (SPAs) with JavaScript Frameworks

**Use:** Encrypted X-XSRF-TOKEN cookie for Angular, Axios, etc.

```go
r := router.New(logger,
    router.WithCSRF(),
)

// Enable X-XSRF-TOKEN cookie in custom middleware setup.
// EncryptionKey is MANDATORY here — see "Encryption key requirement" below.
csrfMW := router.CSRFMiddleware(router.CSRFOptions{
    EnableXSRFCookie: true,
    EncryptionKey:    []byte(os.Getenv("CSRF_ENCRYPTION_KEY")), // exactly 32 bytes
    // Secure cookies are the default. Set InsecureCookie: true only
    // on local-dev plain HTTP — see "Cookie Secure flag" below.
})
mux.Use(csrfMW)
```

**Frontend (Angular/Axios):**
```javascript
// Axios automatically reads X-XSRF-TOKEN cookie
// No additional configuration needed
```

**Why:** JavaScript frameworks like Angular and Axios automatically send the `X-XSRF-TOKEN` header if the cookie exists.

> #### Encryption key requirement (ADR-006, type refined by ADR-008)
>
> When `EnableXSRFCookie` is `true`, `EncryptionKey` **must be exactly 32
> bytes** (AES-256). It is no longer derived from the cookie name — that
> historical default produced a globally-predictable key that an attacker
> could use to forge the `XSRF-TOKEN` cookie offline.
>
> The field type is `[]byte` (ADR-008). Raw key material is bytes, not a
> string; reading the key from an env var or a secret manager gives you
> a string that you wrap in `[]byte(...)` once at construction.
>
> A missing, short, or long key **fails loud at startup**:
> `CSRFMiddleware` panics (the `regexp.MustCompile` pattern); the
> additive `NewCSRFMiddleware` returns `router.ErrCSRFEncryptionKey`
> instead, for callers that prefer to handle the error themselves.
>
> Generate a key once and supply it through the environment / a secret
> manager — never hard-code it:
>
> ```sh
> # 32 random bytes, base64-encoded to 44 printable chars... but the key
> # field wants 32 BYTES. The simplest portable form is 32 raw bytes:
> head -c 32 /dev/urandom | base64   # store this, then base64-decode at load
> # or, if you keep the key as a printable 32-char string:
> LC_ALL=C tr -dc 'A-Za-z0-9' </dev/urandom | head -c 32; echo
> ```
>
> Deployments that do **not** set `EnableXSRFCookie` (the common case)
> are unaffected — `EncryptionKey` stays optional and unused for them.

> #### Cookie Secure flag (ADR-008)
>
> The `_csrf` and `XSRF-TOKEN` cookies are issued with **`Secure: true`
> by default**. The zero-value `CSRFOptions{}` literal — the path
> `router.WithCSRF()` takes — already produces secure cookies that
> refuse to ride over plain HTTP.
>
> Operators running on local-dev plain HTTP opt out explicitly:
>
> ```go
> csrfMW := router.CSRFMiddleware(router.CSRFOptions{
>     InsecureCookie: true, // ONLY for local-dev plain HTTP
> })
> ```
>
> Set `InsecureCookie: true` **only** when serving over plain HTTP
> against a local dev machine. Production must leave it at the default.

> #### Logger (ADR-008)
>
> `CSRFOptions.Logger` is an optional `*slog.Logger`. When nil it
> falls back to `slog.Default()`; `router.DefaultStack` (the chain
> behind `router.WithCSRF`) plumbs the router's logger into the
> middleware automatically.
>
> Log policy:
>
> - Server-side **encrypt failures** (RNG, AES, GCM) log at `WARN` —
>   the `XSRF-TOKEN` cookie is dropped and the operator should see it.
> - **Decrypt failures** on the incoming `X-XSRF-TOKEN` header log at
>   `DEBUG` — every browser stuck with a stale-key cookie and every
>   attacker probe produces one, so they are silenced at production
>   log levels.

---

### 3. APIs with Modern Browsers (Origin-Only Mode)

**Use:** Origin-only verification for HTTPS-only APIs

```go
r := router.New(logger,
    router.WithCSRF("/api/webhook"), // Exempt webhooks
)

// Custom middleware with origin-only mode
csrfMW := router.CSRFMiddleware(router.CSRFOptions{
    EnableOriginCheck: true,
    OriginOnly:        true, // Disable token fallback
})
mux.Use(csrfMW)
```

**Why:** Modern browsers send `Sec-Fetch-Site` header. Origin-only mode simplifies development for APIs served over HTTPS.

**Note:** Origin verification only works over HTTPS connections. Non-HTTPS requests will fall back to token validation if available.

---

### 4. Multi-Tenant Apps with Subdomains

**Use:** Allow same-site requests for subdomain communication

```go
r := router.New(logger,
    router.WithCSRF(),
)

// Enable same-site allowance for subdomain access
csrfMW := router.CSRFMiddleware(router.CSRFOptions{
    EnableOriginCheck: true,
    AllowSameSite:     true, // Allow dashboard.example.com → example.com
})
mux.Use(csrfMW)
```

**Why:** Allows legitimate requests from subdomains (e.g., `dashboard.example.com` to `example.com`) while still blocking cross-site requests.

---

### 5. High-Security Applications (Fintech, Healthcare)

**Use:** Token rotation for enhanced security

```go
r := router.New(logger,
    router.WithCSRF(),
)

// Enable token rotation
csrfMW := router.CSRFMiddleware(router.CSRFOptions{
    UseSessionToken: true, // Store in session
    RotateToken:     true, // Regenerate after each successful validation
})
mux.Use(csrfMW)
```

**Why:** Regenerating the token after each use prevents token reuse attacks, providing defense-in-depth.

**Trade-off:** Users cannot have multiple forms open simultaneously, as each submission invalidates the previous token.

---

### 6. Webhook Endpoints

**Use:** Exempt specific paths from CSRF protection

```go
r := router.New(logger,
    router.WithCSRF(
        "/api/webhook/stripe",    // Stripe webhooks
        "/api/webhook/paypal",    // PayPal webhooks
        "/api/public",            // Public API endpoints
    ),
)
```

**Why:** Webhook providers don't know your CSRF token and cannot send it. Exempt these paths.

---

## Complete Reference

### CSRFOptions

```go
type CSRFOptions struct {
    // Basic options
    ExemptPaths    []string // Paths to skip CSRF validation
    CookieName     string   // CSRF cookie name (default: "_csrf")
    HeaderName     string   // CSRF header name (default: "X-CSRF-Token")
    FormField      string   // CSRF form field name (default: "_csrf_token")
    InsecureCookie bool     // Disable cookie Secure flag — set true ONLY for local-dev plain HTTP (default: false; ADR-008)

    // Origin verification (Laravel-style)
    EnableOriginCheck bool // Enable Sec-Fetch-Site verification (default: true)
    OriginOnly        bool // Use only origin, disable token fallback (default: false)
    AllowSameSite     bool // Allow same-site requests (default: false)

    // Session-based storage
    UseSessionToken bool   // Store token in session instead of cookie (default: false)
    SessionKey      string // Session key (default: "csrf_token")

    // X-XSRF-TOKEN for JS frameworks
    EnableXSRFCookie bool   // Enable encrypted XSRF-TOKEN cookie (default: false)
    XSRFCookieName   string // XSRF-TOKEN cookie name (default: "XSRF-TOKEN")
    EncryptionKey    []byte // AES-256 key — MANDATORY, exactly 32 bytes, when EnableXSRFCookie is true (ADR-006; type refined by ADR-008)

    // Token rotation
    RotateToken bool // Regenerate token after validation (default: false)

    // Observability
    Logger *slog.Logger // Receives WARN on encrypt failures and DEBUG on decrypt failures; defaults to slog.Default() (ADR-008)
}
```

### Constructors

| Constructor | Signature | On misconfiguration |
|-------------|-----------|---------------------|
| `CSRFMiddleware` | `func(CSRFOptions) func(http.Handler) http.Handler` | **panics** at construction (`regexp.MustCompile` pattern) — a bad CSRF config should crash the process at startup, not serve requests with a weak key |
| `NewCSRFMiddleware` | `func(CSRFOptions) (func(http.Handler) http.Handler, error)` | returns `router.ErrCSRFEncryptionKey` — use this when the caller wants to surface the error through its own config validation |

Both apply `defaults()` and the same validation. The only validated misconfiguration today is `EnableXSRFCookie: true` without a 32-byte `EncryptionKey`.

**Security properties (ADR-006):**

- Token comparison is **constant-time** (`crypto/subtle.ConstantTimeCompare`) — the response latency does not leak how many leading bytes of the token an attacker guessed correctly.
- `EncryptionKey` is **never derived** from the cookie name. It must be operator-supplied and exactly 32 bytes when the XSRF cookie is enabled.

### Usage Patterns

#### Pattern A: Default (Recommended)

```go
router.New(logger, router.WithCSRF())
```

**Features:**
- Origin verification enabled
- Cookie-based token storage
- Token validation fallback
- Same-origin only

**Best for:** Most web applications

---

#### Pattern B: Session-Based (More Secure)

```go
csrfMW := router.CSRFMiddleware(router.CSRFOptions{
    UseSessionToken: true,
    // Secure cookies are the default (ADR-008); no explicit field needed.
})
mux.Use(csrfMW)
```

**Features:**
- Origin verification enabled
- Session-based token storage
- Token validation fallback
- Tokens not accessible by JavaScript

**Best for:** Traditional web apps with server-side rendering

---

#### Pattern C: SPA-Friendly

```go
csrfMW := router.CSRFMiddleware(router.CSRFOptions{
    EnableXSRFCookie: true,
    EncryptionKey:    []byte(os.Getenv("CSRF_ENCRYPTION_KEY")),
    // Secure cookies are the default (ADR-008); no explicit field needed.
})
mux.Use(csrfMW)
```

**Features:**
- Origin verification enabled
- Encrypted X-XSRF-TOKEN cookie
- Automatic header sending by Angular/Axios

**Best for:** SPAs with JavaScript frameworks

---

#### Pattern D: Origin-Only (Modern APIs)

```go
csrfMW := router.CSRFMiddleware(router.CSRFOptions{
    EnableOriginCheck: true,
    OriginOnly:        true,
    // Secure cookies are the default (ADR-008); no explicit field needed.
})
mux.Use(csrfMW)
```

**Features:**
- Origin verification only
- No token validation
- Simplified development
- HTTPS required

**Best for:** APIs with modern browser clients

---

#### Pattern E: High-Security

```go
csrfMW := router.CSRFMiddleware(router.CSRFOptions{
    UseSessionToken: true,
    RotateToken:     true,
    // Secure cookies are the default (ADR-008); no explicit field needed.
})
mux.Use(csrfMW)
```

**Features:**
- Session-based storage
- Token rotation
- Maximum security

**Best for:** Fintech, healthcare, high-security applications

---

## Template Integration

### Server-Side Rendering (Go Templates)

```html
<form method="POST" action="/profile">
    <!-- Inject CSRF token -->
    <input type="hidden" name="_csrf_token" value="{{ .CSRFToken }}">
    
    <input type="text" name="name">
    <button type="submit">Submit</button>
</form>
```

### JavaScript/AJAX

```javascript
// Method 1: Read from meta tag
const csrfToken = document.querySelector('meta[name="csrf-token"]').content;

fetch('/api/data', {
    method: 'POST',
    headers: {
        'X-CSRF-Token': csrfToken,
        'Content-Type': 'application/json',
    },
    body: JSON.stringify(data),
});

// Method 2: Automatic with X-XSRF-TOKEN (Angular/Axios)
// No code needed - framework handles it automatically
```

## Testing CSRF Protection

### Test Valid Request

```bash
# Get CSRF token
TOKEN=$(curl -c cookies.txt -s http://localhost:8080/ | grep -oP '_csrf" value="\K[^"]+')

# Submit with token
curl -b cookies.txt -X POST http://localhost:8080/api/data \
    -H "X-CSRF-Token: $TOKEN" \
    -H "Content-Type: application/json" \
    -d '{"key":"value"}'
```

### Test Invalid Request (Should Fail)

```bash
# Submit without token
curl -X POST http://localhost:8080/api/data \
    -H "Content-Type: application/json" \
    -d '{"key":"value"}'

# Expected: 419 CSRF token missing or invalid
```

## Security Best Practices

1. **Always use HTTPS in production** — secure cookies are the default
   (ADR-008); leave `InsecureCookie` at its zero value
2. **Use session-based tokens** when possible — more secure than cookies
3. **Enable origin verification** — reduces load, protects modern browsers
4. **Exempt webhook endpoints** — providers cannot send CSRF tokens
5. **Rotate tokens** for high-security apps — prevents token reuse
6. **Use strong encryption keys** — 32 bytes for AES-256, supplied as `[]byte`
7. **Plumb a logger** so encrypt failures surface in your observability
   stack (`router.WithCSRF` does this automatically)
8. **Test your CSRF protection** — verify exempted paths work correctly

## Comparison with Laravel

| Feature | Nucleus | Laravel |
|---------|---------|---------|
| Origin verification | ✅ Sec-Fetch-Site | ✅ Sec-Fetch-Site |
| Session tokens | ✅ Optional | ✅ Default |
| X-XSRF-TOKEN | ✅ Encrypted | ✅ Encrypted |
| Origin-only mode | ✅ | ✅ |
| Same-site allowance | ✅ | ✅ |
| Token rotation | ✅ Optional | ❌ |
| Cookie-based fallback | ✅ | ❌ |

## Troubleshooting

### Issue: CSRF validation failing on valid requests

**Cause:** Session not configured when using `UseSessionToken: true`

**Solution:**
```go
mux.SetSessionManager(sessionManager)
```

---

### Issue: X-XSRF-TOKEN not being sent

**Cause:** Encryption key invalid or cookie not set

**Solution:**
```go
csrfMW := router.CSRFMiddleware(router.CSRFOptions{
    EnableXSRFCookie: true,
    EncryptionKey:    []byte(os.Getenv("CSRF_ENCRYPTION_KEY")), // Must be exactly 32 bytes
})
```

---

### Issue: Origin verification always failing

**Cause:** Not using HTTPS or browser doesn't send `Sec-Fetch-Site`

**Solution:**
- Ensure HTTPS is enabled in production
- Fallback to token validation is automatic
- Consider `OriginOnly: false` for broader compatibility

---

### Issue: Webhook endpoints rejected

**Cause:** Webhook provider cannot send CSRF token

**Solution:**
```go
router.WithCSRF("/api/webhook/stripe", "/api/webhook/paypal")
```
