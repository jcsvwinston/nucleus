package admin

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/GoFrame/pkg/db"
	"github.com/jcsvwinston/GoFrame/pkg/outbox"
)

func TestBuildSystemEnvironmentRowsMasksSensitiveValues(t *testing.T) {
	rows := buildSystemEnvironmentRows([]string{
		"APP_MODE=dev",
		"API_TOKEN=secret-value",
		"SIGNING_KEY=abc",
	})
	if len(rows) != 3 {
		t.Fatalf("expected three env rows, got %d", len(rows))
	}

	index := map[string]systemEnvVar{}
	for _, row := range rows {
		index[row.Name] = row
	}

	if got := index["APP_MODE"].Value; got != "dev" {
		t.Fatalf("expected APP_MODE=dev, got %q", got)
	}
	if !index["API_TOKEN"].Masked || index["API_TOKEN"].Value != "***" {
		t.Fatalf("expected API_TOKEN masked, got %#v", index["API_TOKEN"])
	}
	if !index["SIGNING_KEY"].Masked || index["SIGNING_KEY"].Value != "***" {
		t.Fatalf("expected SIGNING_KEY masked, got %#v", index["SIGNING_KEY"])
	}
}

func TestGatherGoroutineStateCounts(t *testing.T) {
	rows := gatherGoroutineStateCounts()
	if len(rows) == 0 {
		t.Fatalf("expected at least one state row")
	}
	total := 0
	for _, row := range rows {
		if row.Count <= 0 {
			t.Fatalf("expected positive goroutine count row, got %#v", row)
		}
		total += row.Count
	}
	if total <= 0 {
		t.Fatalf("expected total goroutine count > 0")
	}
}

func TestPanelSystemSnapshotEndpoint(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	panel.config.Databases = []DatabaseRuntimeInfo{
		{Alias: "default", Engine: "sql", Dialect: "sqlite", IsDefault: true},
	}
	panel.config.DatabaseHandles = map[string]*db.DB{"default": panel.db}
	panel.bootEnv = buildSystemEnvironmentRows([]string{
		"APP_ENV=test",
		"SERVICE_TOKEN=token-value",
		"DB_PASSWORD=pass-value",
	})

	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	res, err := http.Get(srv.URL + "/api/system/snapshot?env_limit=50")
	if err != nil {
		t.Fatalf("snapshot request failed: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", res.StatusCode)
	}

	var payload systemSnapshotResponse
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if !payload.Enabled {
		t.Fatalf("expected enabled response")
	}
	if payload.Goroutines.Count <= 0 {
		t.Fatalf("expected goroutines count > 0")
	}
	if len(payload.Databases) == 0 {
		t.Fatalf("expected at least one db pool row")
	}
	if len(payload.Environment) < 2 {
		t.Fatalf("expected environment rows")
	}

	env := map[string]systemEnvVar{}
	for _, row := range payload.Environment {
		env[row.Name] = row
	}
	if env["SERVICE_TOKEN"].Value != "***" || !env["SERVICE_TOKEN"].Masked {
		t.Fatalf("expected SERVICE_TOKEN masked, got %#v", env["SERVICE_TOKEN"])
	}
	if env["DB_PASSWORD"].Value != "***" || !env["DB_PASSWORD"].Masked {
		t.Fatalf("expected DB_PASSWORD masked, got %#v", env["DB_PASSWORD"])
	}
	if env["APP_ENV"].Masked {
		t.Fatalf("expected APP_ENV not masked, got %#v", env["APP_ENV"])
	}
}

func TestPanelSystemSnapshotIncludesJobsAndFlags(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	panel.SetFeatureFlag("checkout_v2", true)
	panel.config.OTLPEndpoint = "https://otel-collector.internal:4318/v1/traces"
	panel.config.TraceURLTemplate = "https://jaeger.example.local/trace/{trace_id}"

	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	res, err := http.Get(srv.URL + "/api/system/snapshot")
	if err != nil {
		t.Fatalf("snapshot request failed: %v", err)
	}
	defer res.Body.Close()

	var payload systemSnapshotResponse
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if payload.Jobs.Enabled {
		t.Fatalf("expected jobs snapshot disabled without redis_url")
	}
	if !strings.Contains(payload.Jobs.Reason, "redis_url") {
		t.Fatalf("expected jobs reason to mention redis_url, got %q", payload.Jobs.Reason)
	}

	if len(payload.Flags) == 0 {
		t.Fatalf("expected at least one feature flag")
	}
	found := false
	for _, row := range payload.Flags {
		if row.Name == "checkout_v2" {
			found = true
			if !row.Enabled {
				t.Fatalf("expected checkout_v2 enabled in snapshot")
			}
		}
	}
	if !found {
		t.Fatalf("expected checkout_v2 flag in snapshot rows")
	}

	if !payload.Telemetry.OTLPConfigured {
		t.Fatalf("expected telemetry.otlp_configured=true")
	}
	if payload.Telemetry.OTLPEndpoint != "https://otel-collector.internal:4318" {
		t.Fatalf("expected summarized otlp endpoint, got %q", payload.Telemetry.OTLPEndpoint)
	}
	if !payload.Telemetry.TraceLinksConfigured {
		t.Fatalf("expected telemetry.trace_links_configured=true")
	}
	if payload.Telemetry.TraceURLTemplate != "https://jaeger.example.local/trace/{trace_id}" {
		t.Fatalf("unexpected trace url template: %q", payload.Telemetry.TraceURLTemplate)
	}
}

func TestPanelSystemSnapshotIncludesOutboxAndClusterTopology(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	panel.config.Databases = []DatabaseRuntimeInfo{
		{Alias: "default", Engine: "sql", Dialect: "sqlite", IsDefault: true},
	}
	panel.config.DatabaseHandles = map[string]*db.DB{"default": panel.db}
	panel.config.LiveClusterEnabled = true

	sqlDB, err := panel.db.SqlDB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	store, err := outbox.NewStore(sqlDB, outbox.Config{Flavor: outbox.FlavorSQLite})
	if err != nil {
		t.Fatalf("new outbox store: %v", err)
	}
	if _, err := store.Enqueue(t.Context(), outbox.Entry{
		Topic:   "emails.send",
		Payload: map[string]any{"to": "dev@example.com"},
	}); err != nil {
		t.Fatalf("enqueue outbox: %v", err)
	}

	now := time.Now().UTC()
	panel.live.nodes.touch(panel.liveNodeID(), now)
	panel.live.nodes.touch("node-b", now.Add(-5*time.Second))
	panel.live.requests.push(liveRequestEvent{
		NodeID:    "node-b",
		Timestamp: now.Format(time.RFC3339),
		Method:    http.MethodGet,
		Path:      "/api/health",
		Status:    http.StatusOK,
	})

	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	res, err := http.Get(srv.URL + "/api/system/snapshot")
	if err != nil {
		t.Fatalf("snapshot request failed: %v", err)
	}
	defer res.Body.Close()

	var payload systemSnapshotResponse
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if !payload.Outbox.Enabled || payload.Outbox.Pending != 1 {
		t.Fatalf("expected outbox pending snapshot, got %#v", payload.Outbox)
	}
	if !payload.Cluster.Enabled {
		t.Fatalf("expected cluster snapshot enabled")
	}
	if len(payload.ClusterNodes) == 0 {
		t.Fatalf("expected cluster nodes in system snapshot")
	}

	foundRemote := false
	for _, node := range payload.ClusterNodes {
		if node.NodeID == "node-b" {
			foundRemote = true
			if node.Requests != 1 {
				t.Fatalf("expected node-b requests=1, got %#v", node)
			}
		}
	}
	if !foundRemote {
		t.Fatalf("expected node-b in cluster nodes: %#v", payload.ClusterNodes)
	}
}

func TestPanelSystemFeatureFlagEndpoints(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	createBody := bytes.NewBufferString(`{"name":"checkout_v2","enabled":true}`)
	createReq, err := http.NewRequest(http.MethodPost, srv.URL+"/api/system/flags", createBody)
	if err != nil {
		t.Fatalf("new request failed: %v", err)
	}
	createReq.Header.Set("Content-Type", "application/json")
	createRes, err := http.DefaultClient.Do(createReq)
	if err != nil {
		t.Fatalf("create request failed: %v", err)
	}
	defer createRes.Body.Close()
	if createRes.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(createRes.Body)
		t.Fatalf("expected status 201 from POST flag, got %d body=%s", createRes.StatusCode, string(raw))
	}

	updateBody := bytes.NewBufferString(`{"enabled":false}`)
	updateReq, err := http.NewRequest(http.MethodPut, srv.URL+"/api/system/flags/checkout_v2", updateBody)
	if err != nil {
		t.Fatalf("new request failed: %v", err)
	}
	updateReq.Header.Set("Content-Type", "application/json")
	updateRes, err := http.DefaultClient.Do(updateReq)
	if err != nil {
		t.Fatalf("put request failed: %v", err)
	}
	defer updateRes.Body.Close()
	if updateRes.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(updateRes.Body)
		t.Fatalf("expected status 200 from PUT flag, got %d body=%s", updateRes.StatusCode, string(raw))
	}

	listRes, err := http.Get(srv.URL + "/api/system/flags")
	if err != nil {
		t.Fatalf("flags list request failed: %v", err)
	}
	defer listRes.Body.Close()

	var listPayload struct {
		Enabled bool               `json:"enabled"`
		Count   int                `json:"count"`
		Flags   []featureFlagState `json:"flags"`
	}
	if err := json.NewDecoder(listRes.Body).Decode(&listPayload); err != nil {
		t.Fatalf("decode list response failed: %v", err)
	}

	if !listPayload.Enabled {
		t.Fatalf("expected flags endpoint enabled")
	}
	if listPayload.Count == 0 || len(listPayload.Flags) == 0 {
		t.Fatalf("expected at least one feature flag in list")
	}
	found := false
	for _, row := range listPayload.Flags {
		if row.Name != "checkout_v2" {
			continue
		}
		found = true
		if row.Enabled {
			t.Fatalf("expected checkout_v2 to be disabled after PUT")
		}
	}
	if !found {
		t.Fatalf("expected checkout_v2 in flags list")
	}

	deleteReq, err := http.NewRequest(http.MethodDelete, srv.URL+"/api/system/flags/checkout_v2", nil)
	if err != nil {
		t.Fatalf("new delete request failed: %v", err)
	}
	deleteRes, err := http.DefaultClient.Do(deleteReq)
	if err != nil {
		t.Fatalf("delete request failed: %v", err)
	}
	defer deleteRes.Body.Close()
	if deleteRes.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(deleteRes.Body)
		t.Fatalf("expected status 200 from DELETE flag, got %d body=%s", deleteRes.StatusCode, string(raw))
	}
}

func TestPanelSystemQueueActionGuards(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()
	panel.config.Environment = "production"
	panel.config.RedisURL = ""

	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	body := bytes.NewBufferString(`{"confirm_queue":"critical","acknowledge":"I_UNDERSTAND_RUNTIME_OPERATION","force":false}`)
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/system/jobs/queues/critical/actions/pause", body)
	if err != nil {
		t.Fatalf("new request failed: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("queue action request failed: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		raw, _ := io.ReadAll(res.Body)
		t.Fatalf("expected status 403 for production without force, got %d body=%s", res.StatusCode, string(raw))
	}

	forcedBody := bytes.NewBufferString(`{"confirm_queue":"critical","acknowledge":"I_UNDERSTAND_RUNTIME_OPERATION","force":true}`)
	forcedReq, err := http.NewRequest(http.MethodPost, srv.URL+"/api/system/jobs/queues/critical/actions/retry-archived", forcedBody)
	if err != nil {
		t.Fatalf("new forced request failed: %v", err)
	}
	forcedReq.Header.Set("Content-Type", "application/json")
	forcedRes, err := http.DefaultClient.Do(forcedReq)
	if err != nil {
		t.Fatalf("forced queue action request failed: %v", err)
	}
	defer forcedRes.Body.Close()
	if forcedRes.StatusCode != http.StatusBadRequest {
		raw, _ := io.ReadAll(forcedRes.Body)
		t.Fatalf("expected status 400 when redis_url missing for supported action, got %d body=%s", forcedRes.StatusCode, string(raw))
	}
}
