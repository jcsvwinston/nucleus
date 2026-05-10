package server_test

// End-to-end Data Studio integration: real admin/agent.Agent (with a
// SQLite-backed model registry) connecting to a real admin/server,
// driven via the DataStudioService Connect-RPC client.
//
// Lives in admin/server/ rather than admin/agent/ because the test
// imports admin/server (which already has integration test
// infrastructure) and admin/agent. Putting it here avoids a circular
// test dependency.

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"testing"
	"time"

	"connectrpc.com/connect"
	_ "modernc.org/sqlite"

	"github.com/jcsvwinston/nucleus/admin/agent"
	"github.com/jcsvwinston/nucleus/pkg/db"
	"github.com/jcsvwinston/nucleus/pkg/model"
	"github.com/jcsvwinston/nucleus/pkg/observability"
	"github.com/jcsvwinston/nucleus/pkg/observe"

	server "github.com/jcsvwinston/nucleus/admin/server"
	adminv1 "github.com/jcsvwinston/nucleus/admin/proto/gen/go/nucleus/admin/v1"
	adminv1connect "github.com/jcsvwinston/nucleus/admin/proto/gen/go/nucleus/admin/v1/adminv1connect"
)

// TestArticle is a minimal model used to drive the Data Studio
// integration test end-to-end. We deliberately do NOT embed BaseModel:
// some pkg/model internals serialize time.Time inconsistently across
// drivers, which is orthogonal to what we want to test here. The
// integration target is "the request reaches the agent's CRUD and the
// response makes it back through the bidi stream", not the framework's
// timestamp story.
type TestArticle struct {
	ID    uint   `db:"pk;column:id" json:"id"`
	Title string `db:"column:title;required" json:"title"`
	Body  string `db:"column:body" json:"body"`
}

func setupAgentDB(t *testing.T) (*db.DB, *model.Registry) {
	t.Helper()
	logger := observe.NewLogger("error", "text")
	cfg := db.Config{
		Engine:          db.EngineSQL,
		DatabaseURL:     "sqlite://:memory:",
		DatabaseMaxOpen: 1,
		DatabaseMaxIdle: 1,
	}
	d, err := db.New(cfg, logger)
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	sqlDB, err := d.SqlDB()
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, sqlDB, `CREATE TABLE test_articles (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT NOT NULL,
		body TEXT
	)`)
	for i := 1; i <= 3; i++ {
		mustExec(t, sqlDB,
			`INSERT INTO test_articles (id, title, body) VALUES (?, ?, ?)`,
			i,
			"seed article "+strconv.Itoa(i),
			"body "+strconv.Itoa(i),
		)
	}

	reg := model.NewRegistry()
	if err := reg.Register(&TestArticle{}); err != nil {
		t.Fatalf("register: %v", err)
	}
	return d, reg
}

func mustExec(t *testing.T, sqlDB *sql.DB, q string, args ...interface{}) {
	t.Helper()
	if _, err := sqlDB.Exec(q, args...); err != nil {
		t.Fatalf("exec %q: %v", q, err)
	}
}

func startServerAndAgent(t *testing.T) (srv *server.Server, ag *agent.Agent, stop func()) {
	t.Helper()

	srvLogger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv = server.New(server.Config{
		AgentAddr: "127.0.0.1:0",
		UIAddr:    "127.0.0.1:0",
		Logger:    srvLogger,
	})

	srvCtx, srvCancel := context.WithCancel(context.Background())
	srvDone := make(chan error, 1)
	go func() { srvDone <- srv.Run(srvCtx) }()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && (srv.AgentAddr() == "" || srv.UIAddr() == "") {
		time.Sleep(20 * time.Millisecond)
	}
	if srv.AgentAddr() == "" {
		srvCancel()
		<-srvDone
		t.Fatal("server did not bind")
	}

	d, reg := setupAgentDB(t)
	bus := observability.NewBus(slog.New(slog.NewTextHandler(io.Discard, nil)))

	ag, err := agent.New(agent.Config{
		Endpoints:         []string{"http://" + srv.AgentAddr()},
		Bus:               bus,
		Registry:          reg,
		Databases:         map[string]*db.DB{"default": d},
		StateDir:          t.TempDir(),
		HeartbeatInterval: 100 * time.Millisecond,
		DrainTimeout:      500 * time.Millisecond,
		Logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		srvCancel()
		<-srvDone
		t.Fatal(err)
	}

	agCtx, agCancel := context.WithCancel(context.Background())
	agDone := make(chan error, 1)
	go func() { agDone <- ag.Run(agCtx) }()

	// Wait until the server has the agent registered.
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := srv.State().Nodes.Lookup(ag.NodeID()); ok {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, ok := srv.State().Nodes.Lookup(ag.NodeID()); !ok {
		agCancel()
		srvCancel()
		t.Fatal("agent did not register with server in 3s")
	}

	stop = func() {
		agCancel()
		<-agDone
		srvCancel()
		<-srvDone
	}
	return srv, ag, stop
}

func newDataStudioClient(t *testing.T, uiURL string) adminv1connect.DataStudioServiceClient {
	t.Helper()
	return adminv1connect.NewDataStudioServiceClient(uiH2CClient(), uiURL)
}

// TestDataStudio_ListModels verifies the server fast path returns the
// registered model union without hitting an agent.
func TestDataStudio_ListModels(t *testing.T) {
	srv, _, stop := startServerAndAgent(t)
	defer stop()

	client := newDataStudioClient(t, "http://"+srv.UIAddr())
	resp, err := client.ListModels(context.Background(), connect.NewRequest(&adminv1.ListModelsRequest{}))
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	got := resp.Msg.GetModels()
	if len(got) != 1 {
		t.Fatalf("got %d models, want 1: %+v", len(got), got)
	}
	if got[0].Name != "TestArticle" {
		t.Errorf("got %q, want TestArticle", got[0].Name)
	}
}

// TestDataStudio_GetSchema verifies schema retrieval via the agent
// proxy.
func TestDataStudio_GetSchema(t *testing.T) {
	srv, _, stop := startServerAndAgent(t)
	defer stop()

	client := newDataStudioClient(t, "http://"+srv.UIAddr())
	resp, err := client.GetSchema(context.Background(), connect.NewRequest(&adminv1.GetSchemaRequest{
		ModelName: "TestArticle",
	}))
	if err != nil {
		t.Fatalf("GetSchema: %v", err)
	}
	schema := resp.Msg
	if schema.Info.Name != "TestArticle" {
		t.Errorf("info name = %q", schema.Info.Name)
	}
	fields := schema.GetFields()
	if len(fields) < 3 {
		t.Errorf("expected at least 3 fields, got %d", len(fields))
	}
	hasTitle := false
	for _, f := range fields {
		if f.Name == "Title" {
			hasTitle = true
			break
		}
	}
	if !hasTitle {
		t.Errorf("Title field missing from schema: %+v", fields)
	}
}

// TestDataStudio_ListRecords pulls the seeded rows via the full
// UI -> server -> agent -> CRUD path.
func TestDataStudio_ListRecords(t *testing.T) {
	srv, _, stop := startServerAndAgent(t)
	defer stop()

	client := newDataStudioClient(t, "http://"+srv.UIAddr())
	resp, err := client.ListRecords(context.Background(), connect.NewRequest(&adminv1.ListRecordsRequest{
		ModelName: "TestArticle",
		Page:      1,
		PageSize:  10,
	}))
	if err != nil {
		t.Fatalf("ListRecords: %v", err)
	}
	if got := len(resp.Msg.GetItems()); got != 3 {
		t.Fatalf("got %d records, want 3", got)
	}
}

// TestDataStudio_CreateUpdateDelete drives the full write surface.
func TestDataStudio_CreateUpdateDelete(t *testing.T) {
	srv, _, stop := startServerAndAgent(t)
	defer stop()

	client := newDataStudioClient(t, "http://"+srv.UIAddr())

	// Create
	created, err := client.CreateRecord(context.Background(), connect.NewRequest(&adminv1.CreateRecordRequest{
		ModelName: "TestArticle",
		Record: &adminv1.Record{ValuesJson: map[string]string{
			"Title": `"created via UI"`,
			"Body":  `"hello"`,
		}},
	}))
	if err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}
	id := unquote(created.Msg.GetValuesJson()["ID"])
	if id == "" {
		t.Fatalf("created record has no ID: %+v", created.Msg.GetValuesJson())
	}

	// Update
	_, err = client.UpdateRecord(context.Background(), connect.NewRequest(&adminv1.UpdateRecordRequest{
		ModelName: "TestArticle",
		Id:        id,
		Record: &adminv1.Record{ValuesJson: map[string]string{
			"Title": `"updated"`,
		}},
	}))
	if err != nil {
		t.Fatalf("UpdateRecord: %v", err)
	}

	// Get to verify update
	got, err := client.GetRecord(context.Background(), connect.NewRequest(&adminv1.GetRecordRequest{
		ModelName: "TestArticle",
		Id:        id,
	}))
	if err != nil {
		t.Fatalf("GetRecord: %v", err)
	}
	if title := unquote(got.Msg.GetValuesJson()["Title"]); title != "updated" {
		t.Errorf("title after update = %q", title)
	}

	// Delete
	del, err := client.DeleteRecord(context.Background(), connect.NewRequest(&adminv1.DeleteRecordRequest{
		ModelName: "TestArticle",
		Id:        id,
	}))
	if err != nil {
		t.Fatalf("DeleteRecord: %v", err)
	}
	if !del.Msg.GetDeleted() {
		t.Error("Deleted = false")
	}

	// Confirm gone
	_, err = client.GetRecord(context.Background(), connect.NewRequest(&adminv1.GetRecordRequest{
		ModelName: "TestArticle",
		Id:        id,
	}))
	if err == nil {
		t.Error("expected error fetching deleted record")
	}
}

func unquote(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

// silence unused-import warning when subset of helpers is touched.
var _ = fmt.Sprintf
