// Package sampler implements per-kind sampling and post-bus filtering for
// the agent.
//
// The bus's own Filter narrows by EventKind and NodeID — coarse-grained,
// fast. The Sampler refines this with two further dimensions the bus
// deliberately does not know about:
//
//   - Per-kind sampling rate (0.0–1.0). The admin server may say
//     "ship 10 % of SQL events but 100 % of HTTP" because SQL volume can
//     overwhelm the channel during normal load.
//   - HTTP-specific filters (path glob, method, status class) and SQL-
//     specific filters (model name) that the bus is intentionally agnostic
//     about. Forwarding these to the bus would couple it to wire concepts
//     it does not own.
//
// Sampler is goroutine-safe; the rate map and the filter are read on every
// event. Rates and filters can be updated atomically via Update.
package sampler

import (
	"math/rand/v2"
	"path"
	"strings"
	"sync"

	"github.com/jcsvwinston/nucleus/pkg/observability"

	adminv1 "github.com/jcsvwinston/nucleus/admin/proto/gen/go/nucleus/admin/v1"
)

// Decision is the outcome of running the sampler over one event.
type Decision uint8

const (
	// Accept means the event passes; the agent should forward it.
	Accept Decision = iota
	// DropSampled means the per-kind rate randomly excluded this event.
	DropSampled
	// DropFiltered means a wire-level filter rejected it.
	DropFiltered
)

// Sampler groups one filter and its sampling rates, behind an RWMutex so
// the server-driven Subscribe can update it without tearing.
type Sampler struct {
	mu         sync.RWMutex
	filter     *adminv1.Filter   // wire filter (full proto, including HTTP/SQL specifics)
	rates      map[string]float32 // EventKind enum-name without prefix, e.g. "HTTP_REQUEST"
}

// New constructs a Sampler with the given starting filter and rate map.
// Pass nil filter for "no constraint" and nil rates for "100 % all kinds".
func New(filter *adminv1.Filter, rates map[string]float32) *Sampler {
	s := &Sampler{}
	s.Update(filter, rates)
	return s
}

// Update replaces the filter and rates atomically.
func (s *Sampler) Update(filter *adminv1.Filter, rates map[string]float32) {
	if s == nil {
		return
	}
	r := make(map[string]float32, len(rates))
	for k, v := range rates {
		key := strings.ToUpper(strings.TrimSpace(k))
		if key == "" {
			continue
		}
		// Clamp 0..1
		if v < 0 {
			v = 0
		} else if v > 1 {
			v = 1
		}
		r[key] = v
	}

	s.mu.Lock()
	s.filter = filter
	s.rates = r
	s.mu.Unlock()
}

// Decide runs the rate roll first (cheap), then the filter (more work).
// Returns one of Accept / DropSampled / DropFiltered. The caller is
// responsible for incrementing the right counter.
func (s *Sampler) Decide(e observability.Event) Decision {
	if s == nil || e == nil {
		return Accept
	}

	rate := s.rateFor(e.Kind())
	if rate < 1.0 {
		if rate <= 0 {
			return DropSampled
		}
		// rand/v2 is goroutine-safe.
		if rand.Float32() >= rate {
			return DropSampled
		}
	}

	if !s.filterMatches(e) {
		return DropFiltered
	}
	return Accept
}

func (s *Sampler) rateFor(kind observability.EventKind) float32 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.rates) == 0 {
		return 1.0
	}
	key := rateKeyFromKind(kind)
	if v, ok := s.rates[key]; ok {
		return v
	}
	return 1.0
}

// rateKeyFromKind returns the rate-map key for an EventKind. The keys are
// the upper-snake-case suffix of the proto enum (e.g.
// "EVENT_TYPE_HTTP_REQUEST" → "HTTP_REQUEST"). This matches the contract
// documented in admin.proto on Subscribe.sampling_rate.
func rateKeyFromKind(k observability.EventKind) string {
	switch k {
	case observability.KindHTTPRequest:
		return "HTTP_REQUEST"
	case observability.KindSQLStatement:
		return "SQL_STATEMENT"
	case observability.KindSessionChange:
		return "SESSION_CHANGE"
	case observability.KindCustom:
		return "CUSTOM"
	default:
		return ""
	}
}

func (s *Sampler) filterMatches(e observability.Event) bool {
	s.mu.RLock()
	f := s.filter
	s.mu.RUnlock()

	if f == nil {
		return true
	}

	// HTTP-specific filters are only meaningful for HTTP events; for other
	// kinds, the HTTP filters (if any) are ignored.
	if http, ok := e.(*observability.HTTPRequestEvent); ok {
		if !methodMatches(f.HttpMethods, http.Method) {
			return false
		}
		if !pathMatches(f.HttpPathGlobs, http.Path) {
			return false
		}
		if !statusClassMatches(f.HttpStatusClasses, http.Status) {
			return false
		}
	}

	if sql, ok := e.(*observability.SQLStatementEvent); ok {
		if !sqlModelMatches(f.SqlModels, sql.ModelName) {
			return false
		}
	}

	return true
}

func methodMatches(allow []string, method string) bool {
	if len(allow) == 0 {
		return true
	}
	for _, a := range allow {
		if strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(method)) {
			return true
		}
	}
	return false
}

func pathMatches(globs []string, requestPath string) bool {
	if len(globs) == 0 {
		return true
	}
	value := strings.TrimSpace(requestPath)
	for _, glob := range globs {
		glob = strings.TrimSpace(glob)
		if glob == "" {
			continue
		}
		if glob == "*" {
			return true
		}
		if strings.HasSuffix(glob, "/*") {
			prefix := strings.TrimSuffix(glob, "/*")
			if prefix == "" || prefix == "/" {
				return true
			}
			if value == prefix || strings.HasPrefix(value, prefix+"/") {
				return true
			}
			continue
		}
		if strings.ContainsAny(glob, "*?") {
			if matched, _ := path.Match(glob, value); matched {
				return true
			}
			continue
		}
		if value == glob || strings.HasPrefix(value, strings.TrimRight(glob, "/")+"/") {
			return true
		}
	}
	return false
}

func statusClassMatches(classes []string, status int) bool {
	if len(classes) == 0 {
		return true
	}
	for _, c := range classes {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		// "5", "5xx", "503" all interpreted by leading digit; if length is
		// >1 and rest is alpha, we treat it as class.
		if len(c) >= 1 {
			lead := c[0]
			if lead < '0' || lead > '9' {
				continue
			}
			if int(lead-'0') == status/100 {
				return true
			}
			// Exact match (e.g. "503" == 503).
			if len(c) == 3 {
				exact := int(c[0]-'0')*100 + int(c[1]-'0')*10 + int(c[2]-'0')
				if exact == status {
					return true
				}
			}
		}
	}
	return false
}

func sqlModelMatches(allow []string, model string) bool {
	if len(allow) == 0 {
		return true
	}
	target := strings.TrimSpace(model)
	for _, a := range allow {
		if strings.EqualFold(strings.TrimSpace(a), target) {
			return true
		}
	}
	return false
}
