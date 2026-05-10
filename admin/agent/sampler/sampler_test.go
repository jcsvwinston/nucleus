package sampler

import (
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/observability"

	adminv1 "github.com/jcsvwinston/nucleus/admin/proto/gen/go/nucleus/admin/v1"
)

func newHTTP(method, path string, status int) *observability.HTTPRequestEvent {
	e := observability.AcquireHTTPRequestEvent(time.Now(), "n")
	e.Method = method
	e.Path = path
	e.Status = status
	return e
}

func TestSampler_NilFilter_AcceptsAll(t *testing.T) {
	s := New(nil, nil)
	for _, e := range []observability.Event{
		newHTTP("GET", "/foo", 200),
		newHTTP("POST", "/bar", 500),
	} {
		if got := s.Decide(e); got != Accept {
			t.Errorf("expected Accept, got %v", got)
		}
		e.Release()
	}
}

func TestSampler_RateZero_AlwaysDrops(t *testing.T) {
	s := New(nil, map[string]float32{"HTTP_REQUEST": 0})

	for i := 0; i < 100; i++ {
		e := newHTTP("GET", "/", 200)
		if got := s.Decide(e); got != DropSampled {
			t.Errorf("iter %d: got %v, want DropSampled", i, got)
		}
		e.Release()
	}
}

func TestSampler_RateOne_AlwaysAccepts(t *testing.T) {
	s := New(nil, map[string]float32{"HTTP_REQUEST": 1.0})

	for i := 0; i < 50; i++ {
		e := newHTTP("GET", "/", 200)
		if got := s.Decide(e); got != Accept {
			t.Errorf("iter %d: got %v, want Accept", i, got)
		}
		e.Release()
	}
}

func TestSampler_OtherKindUnaffectedByRate(t *testing.T) {
	s := New(nil, map[string]float32{"HTTP_REQUEST": 0})
	e := observability.AcquireSQLStatementEvent(time.Now(), "n")
	defer e.Release()

	if got := s.Decide(e); got != Accept {
		t.Errorf("SQL with HTTP rate=0 should still pass, got %v", got)
	}
}

func TestSampler_MethodFilter(t *testing.T) {
	s := New(&adminv1.Filter{HttpMethods: []string{"POST", "PUT"}}, nil)

	get := newHTTP("GET", "/x", 200)
	defer get.Release()
	post := newHTTP("POST", "/x", 200)
	defer post.Release()

	if s.Decide(get) != DropFiltered {
		t.Error("GET should be filtered out")
	}
	if s.Decide(post) != Accept {
		t.Error("POST should pass")
	}
}

func TestSampler_PathGlobs(t *testing.T) {
	s := New(&adminv1.Filter{HttpPathGlobs: []string{"/api/*", "/healthz"}}, nil)

	cases := []struct {
		path string
		want Decision
	}{
		{"/api/things", Accept},
		{"/api/things/1", Accept},
		{"/api", Accept}, // exact prefix match
		{"/healthz", Accept},
		{"/admin", DropFiltered},
		{"/api-v2/x", DropFiltered},
	}
	for _, tc := range cases {
		e := newHTTP("GET", tc.path, 200)
		got := s.Decide(e)
		if got != tc.want {
			t.Errorf("path=%q got=%v want=%v", tc.path, got, tc.want)
		}
		e.Release()
	}
}

func TestSampler_StatusClass(t *testing.T) {
	s := New(&adminv1.Filter{HttpStatusClasses: []string{"5"}}, nil)

	e2xx := newHTTP("GET", "/", 200)
	defer e2xx.Release()
	e5xx := newHTTP("GET", "/", 500)
	defer e5xx.Release()

	if s.Decide(e2xx) != DropFiltered {
		t.Error("2xx should be filtered when only 5xx allowed")
	}
	if s.Decide(e5xx) != Accept {
		t.Error("500 should pass when 5xx allowed")
	}
}

func TestSampler_StatusClass_ExactMatch(t *testing.T) {
	s := New(&adminv1.Filter{HttpStatusClasses: []string{"503"}}, nil)

	e503 := newHTTP("GET", "/", 503)
	defer e503.Release()
	e500 := newHTTP("GET", "/", 500)
	defer e500.Release()

	if s.Decide(e503) != Accept {
		t.Error("exact 503 must accept")
	}
	if s.Decide(e500) != Accept {
		// "503" implies leading digit '5' as well, so 500 also matches.
		t.Error("500 should also accept (leading digit class)")
	}
}

func TestSampler_SQLModelFilter(t *testing.T) {
	s := New(&adminv1.Filter{SqlModels: []string{"Article", "User"}}, nil)

	a := observability.AcquireSQLStatementEvent(time.Now(), "n")
	a.ModelName = "Article"
	defer a.Release()

	c := observability.AcquireSQLStatementEvent(time.Now(), "n")
	c.ModelName = "Comment"
	defer c.Release()

	if s.Decide(a) != Accept {
		t.Error("Article should pass")
	}
	if s.Decide(c) != DropFiltered {
		t.Error("Comment should be filtered")
	}
}

func TestSampler_Update(t *testing.T) {
	s := New(nil, nil)

	e := newHTTP("GET", "/", 200)
	defer e.Release()
	if got := s.Decide(e); got != Accept {
		t.Fatalf("expected Accept before update, got %v", got)
	}

	s.Update(nil, map[string]float32{"HTTP_REQUEST": 0})
	if got := s.Decide(e); got != DropSampled {
		t.Errorf("after update: got %v, want DropSampled", got)
	}
}

func TestSampler_NilSafe(t *testing.T) {
	var s *Sampler
	e := newHTTP("GET", "/", 200)
	defer e.Release()
	if got := s.Decide(e); got != Accept {
		t.Errorf("nil sampler should accept, got %v", got)
	}
}
