package convert

import (
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/observability"

	adminv1 "github.com/jcsvwinston/nucleus/admin/proto/gen/go/nucleus/admin/v1"
)

func TestEventToProto_HTTP(t *testing.T) {
	now := time.Now().UTC()
	e := observability.AcquireHTTPRequestEvent(now, "node-a")
	defer e.Release()
	e.Method = "POST"
	e.Path = "/api/things"
	e.Status = 201
	e.Duration = 5 * time.Millisecond
	e.RequestID = "req-1"
	e.TraceID = "trace-1"
	e.UserID = "u-1"
	e.RemoteIP = "1.2.3.4"
	e.UserAgent = "ua/1"
	e.PayloadPreview = "body:redacted"

	p := EventToProto(e)
	if p == nil {
		t.Fatal("got nil")
	}
	if p.NodeId != "node-a" {
		t.Errorf("NodeId = %q", p.NodeId)
	}
	body := p.GetHttpRequest()
	if body == nil {
		t.Fatal("missing http_request body")
	}
	if body.Method != "POST" || body.Status != 201 {
		t.Errorf("body = %+v", body)
	}
	if body.Duration.AsDuration() != 5*time.Millisecond {
		t.Errorf("duration = %v", body.Duration.AsDuration())
	}
}

func TestEventToProto_SQL(t *testing.T) {
	e := observability.AcquireSQLStatementEvent(time.Now(), "node-b")
	defer e.Release()
	e.ModelName = "Article"
	e.Operation = "select"
	e.Query = "SELECT * FROM articles"
	e.Args = append(e.Args, "int:1", "string(5):***")
	e.Duration = 2 * time.Millisecond
	e.Err = "boom"

	p := EventToProto(e)
	body := p.GetSqlStatement()
	if body == nil {
		t.Fatal("missing sql_statement body")
	}
	if body.ModelName != "Article" || body.Operation != "select" {
		t.Errorf("body = %+v", body)
	}
	if len(body.Args) != 2 {
		t.Errorf("args = %v", body.Args)
	}
	if body.Error != "boom" {
		t.Errorf("error = %q", body.Error)
	}
}

func TestEventToProto_Session(t *testing.T) {
	e := observability.AcquireSessionChangeEvent(time.Now(), "node-c")
	defer e.Release()
	e.Change = observability.SessionChangeCreated
	e.TokenShort = "ab12cd34"
	e.UserID = "u-2"

	p := EventToProto(e)
	body := p.GetSessionChange()
	if body == nil {
		t.Fatal("missing session_change body")
	}
	if body.Kind != adminv1.SessionChangeEvent_KIND_CREATED {
		t.Errorf("kind = %v", body.Kind)
	}
	if body.TokenShort != "ab12cd34" {
		t.Errorf("token = %q", body.TokenShort)
	}
}

func TestEventToProto_Custom(t *testing.T) {
	e := observability.AcquireCustomEvent(time.Now(), "node-d")
	defer e.Release()
	e.Name = "domain.event"
	e.Labels = map[string]string{"k": "v"}
	e.Payload = append(e.Payload, []byte(`{"x":1}`)...)
	e.ContentType = "application/json"

	p := EventToProto(e)
	body := p.GetCustom()
	if body == nil {
		t.Fatal("missing custom body")
	}
	if body.Name != "domain.event" {
		t.Errorf("name = %q", body.Name)
	}
	if string(body.Payload) != `{"x":1}` {
		t.Errorf("payload = %q", string(body.Payload))
	}
	if body.Labels["k"] != "v" {
		t.Errorf("label k = %q", body.Labels["k"])
	}
}

func TestEventToProto_NilSafe(t *testing.T) {
	if EventToProto(nil) != nil {
		t.Fatal("nil input should yield nil output")
	}
}

func TestKindToProto_RoundTrip(t *testing.T) {
	for _, k := range []observability.EventKind{
		observability.KindHTTPRequest,
		observability.KindSQLStatement,
		observability.KindSessionChange,
		observability.KindCustom,
	} {
		p := KindToProto(k)
		if got := KindFromProto(p); got != k {
			t.Errorf("round-trip %s -> %v -> %s", k, p, got)
		}
	}
	if KindFromProto(adminv1.EventType_EVENT_TYPE_UNSPECIFIED) != observability.KindUnknown {
		t.Error("unspecified did not map to unknown")
	}
}

func TestFilterFromProto(t *testing.T) {
	in := &adminv1.Filter{
		Types:   []adminv1.EventType{adminv1.EventType_EVENT_TYPE_HTTP_REQUEST, adminv1.EventType_EVENT_TYPE_SQL_STATEMENT},
		NodeIds: []string{"node-a", "node-b"},
	}
	out := FilterFromProto(in)
	if len(out.Kinds) != 2 || out.Kinds[0] != observability.KindHTTPRequest {
		t.Errorf("kinds = %v", out.Kinds)
	}
	if len(out.NodeIDs) != 2 || out.NodeIDs[0] != "node-a" {
		t.Errorf("nodes = %v", out.NodeIDs)
	}

	if got := FilterFromProto(nil); len(got.Kinds) != 0 || len(got.NodeIDs) != 0 {
		t.Errorf("nil input should yield empty filter, got %+v", got)
	}
}
