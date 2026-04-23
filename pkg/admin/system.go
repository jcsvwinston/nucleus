package admin

import (
	"bytes"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"runtime/pprof"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jcsvwinston/GoFrame/pkg/db"
	"github.com/jcsvwinston/GoFrame/pkg/outbox"
	"github.com/jcsvwinston/GoFrame/pkg/tasks"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/process"
)

const (
	defaultSystemEnvLimit = 200
	maxSystemEnvLimit     = 2000
)

type systemSnapshotResponse struct {
	Enabled        bool                    `json:"enabled"`
	GeneratedAt    string                  `json:"generated_at"`
	GoVersion      string                  `json:"go_version"`
	GoOS           string                  `json:"go_os"`
	GoArch         string                  `json:"go_arch"`
	GOMAXPROCS     int                     `json:"gomaxprocs"`
	CPUs           int                     `json:"cpus"`
	CPULoad        float64                 `json:"cpu_load"`
	ProcessCPULoad float64                 `json:"process_cpu_load"`
	Goroutines     systemGoroutinesInfo    `json:"goroutines"`
	Memory         systemMemoryInfo        `json:"memory"`
	Databases      []systemDatabasePoolRow `json:"databases"`
	Jobs           tasks.RuntimeSnapshot   `json:"jobs"`
	Outbox         outbox.RuntimeSnapshot  `json:"outbox"`
	Cluster        liveClusterSnapshot     `json:"cluster"`
	ClusterNodes   []liveNodeSnapshot      `json:"cluster_nodes"`
	Flags          []featureFlagState      `json:"flags"`
	Telemetry      systemTelemetryInfo     `json:"telemetry"`
	Environment    []systemEnvVar          `json:"environment"`
}

type systemGoroutinesInfo struct {
	Count       int                `json:"count"`
	StateCounts []systemStateCount `json:"state_counts"`
}

type systemStateCount struct {
	State string `json:"state"`
	Count int    `json:"count"`
}

type systemMemoryInfo struct {
	AllocBytes      uint64 `json:"alloc_bytes"`
	HeapAllocBytes  uint64 `json:"heap_alloc_bytes"`
	HeapSysBytes    uint64 `json:"heap_sys_bytes"`
	StackInUseBytes uint64 `json:"stack_in_use_bytes"`
	HeapObjects     uint64 `json:"heap_objects"`
	NumGC           uint32 `json:"num_gc"`
	LastPauseMS     uint64 `json:"last_pause_ms"`
	PauseTotalMS    uint64 `json:"pause_total_ms"`
}

type systemDatabasePoolRow struct {
	Alias              string `json:"alias"`
	Engine             string `json:"engine"`
	Dialect            string `json:"dialect"`
	IsDefault          bool   `json:"is_default"`
	OpenConnections    int    `json:"open_connections"`
	InUse              int    `json:"in_use"`
	Idle               int    `json:"idle"`
	WaitCount          int64  `json:"wait_count"`
	WaitDurationMS     int64  `json:"wait_duration_ms"`
	MaxOpenConnections int    `json:"max_open_connections"`
	Error              string `json:"error,omitempty"`
}

type systemEnvVar struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Masked bool   `json:"masked"`
}

type systemTelemetryInfo struct {
	OTLPConfigured       bool   `json:"otlp_configured"`
	OTLPEndpoint         string `json:"otlp_endpoint,omitempty"`
	TraceLinksConfigured bool   `json:"trace_links_configured"`
	TraceURLTemplate     string `json:"trace_url_template,omitempty"`
}

func (p *Panel) handleSystemSnapshot(w http.ResponseWriter, r *http.Request) {
	if !p.authorizeAction(w, r, "*", "system_pulse") {
		return
	}

	limit := parseSystemEnvLimit(r, defaultSystemEnvLimit)
	mem := runtime.MemStats{}
	runtime.ReadMemStats(&mem)

	resp := systemSnapshotResponse{
		Enabled:        true,
		GeneratedAt:    time.Now().UTC().Format(time.RFC3339),
		GoVersion:      runtime.Version(),
		GoOS:           runtime.GOOS,
		GoArch:         runtime.GOARCH,
		GOMAXPROCS:     runtime.GOMAXPROCS(0),
		CPUs:           runtime.NumCPU(),
		CPULoad:        getCPULoad(),
		ProcessCPULoad: getProcessCPULoad(),
		Goroutines: systemGoroutinesInfo{
			Count:       runtime.NumGoroutine(),
			StateCounts: gatherGoroutineStateCounts(),
		},
		Memory: systemMemoryInfo{
			AllocBytes:      mem.Alloc,
			HeapAllocBytes:  mem.HeapAlloc,
			HeapSysBytes:    mem.HeapSys,
			StackInUseBytes: mem.StackInuse,
			HeapObjects:     mem.HeapObjects,
			NumGC:           mem.NumGC,
			LastPauseMS:     lastGCPauseMS(mem),
			PauseTotalMS:    uint64(mem.PauseTotalNs / uint64(time.Millisecond)),
		},
		Databases:    p.systemDatabasePoolRows(),
		Jobs:         tasks.InspectRuntime(p.config.RedisURL),
		Outbox:       p.systemOutboxSnapshot(),
		Cluster:      p.liveClusterSnapshot(),
		ClusterNodes: p.systemClusterNodes(time.Now().UTC()),
		Flags:        p.systemFeatureFlags(),
		Telemetry: systemTelemetryInfo{
			OTLPConfigured:       strings.TrimSpace(p.config.OTLPEndpoint) != "",
			OTLPEndpoint:         summarizeOTLPEndpoint(p.config.OTLPEndpoint),
			TraceLinksConfigured: strings.TrimSpace(p.config.TraceURLTemplate) != "",
			TraceURLTemplate:     strings.TrimSpace(p.config.TraceURLTemplate),
		},
		Environment: p.systemEnvironmentRows(limit),
	}

	writeJSON(w, http.StatusOK, resp)
}

func (p *Panel) systemOutboxSnapshot() outbox.RuntimeSnapshot {
	if p == nil {
		return outbox.RuntimeSnapshot{Enabled: false, Table: outbox.DefaultTableName, Reason: "admin panel is not initialized"}
	}
	info, ok := p.defaultSystemDatabaseInfo()
	if !ok {
		return outbox.RuntimeSnapshot{Enabled: false, Table: outbox.DefaultTableName, Reason: "default database runtime is not configured"}
	}
	handle := p.lookupSystemDBHandle(info)
	if handle == nil {
		return outbox.RuntimeSnapshot{Enabled: false, Table: outbox.DefaultTableName, Reason: "database handle not available"}
	}
	sqlDB, err := handle.SqlDB()
	if err != nil {
		return outbox.RuntimeSnapshot{Enabled: false, Table: outbox.DefaultTableName, Reason: err.Error()}
	}
	return outbox.InspectRuntime(sqlDB, outbox.Config{
		TableName: outbox.DefaultTableName,
		Flavor:    outboxFlavorForDialect(info.Dialect),
	})
}

func (p *Panel) systemClusterNodes(now time.Time) []liveNodeSnapshot {
	if p == nil || p.live == nil {
		return []liveNodeSnapshot{}
	}
	requestStats := p.live.requests.stats()
	sqlStats := p.live.sql.stats()
	allRequests := p.live.requests.latestFilteredByNode(requestStats.Stored, p.liveExcludePatterns(), "")
	allQueries := p.live.sql.latest(sqlStats.Stored)
	allSessions := p.live.sessions.snapshot(maxLiveListLimit)
	activeNodes := p.live.nodes.active(liveNodeDegradedWindow)
	return buildLiveNodeSnapshots(now, p.liveNodeID(), activeNodes, allRequests, allQueries, allSessions)
}

func (p *Panel) systemEnvironmentRows(limit int) []systemEnvVar {
	rows := p.bootEnv
	if len(rows) == 0 {
		rows = buildSystemEnvironmentRows(os.Environ())
	}
	if limit <= 0 || len(rows) <= limit {
		out := make([]systemEnvVar, len(rows))
		copy(out, rows)
		return out
	}
	out := make([]systemEnvVar, limit)
	copy(out, rows[:limit])
	return out
}

func (p *Panel) systemDatabasePoolRows() []systemDatabasePoolRow {
	dbInfos := p.config.Databases
	if len(dbInfos) == 0 {
		dbInfos = []DatabaseRuntimeInfo{
			{Alias: "default", Engine: "sql", Dialect: "unknown", IsDefault: true},
		}
	}

	rows := make([]systemDatabasePoolRow, 0, len(dbInfos))
	for _, info := range dbInfos {
		row := systemDatabasePoolRow{
			Alias:     strings.TrimSpace(info.Alias),
			Engine:    strings.TrimSpace(info.Engine),
			Dialect:   strings.TrimSpace(info.Dialect),
			IsDefault: info.IsDefault,
		}
		if row.Alias == "" {
			row.Alias = "default"
		}
		if row.Engine == "" {
			row.Engine = "sql"
		}
		if row.Dialect == "" {
			row.Dialect = "unknown"
		}

		handle := p.lookupSystemDBHandle(info)
		if handle == nil {
			row.Error = "database handle not available"
			rows = append(rows, row)
			continue
		}
		sqlDB, err := handle.SqlDB()
		if err != nil {
			row.Error = err.Error()
			rows = append(rows, row)
			continue
		}
		stats := sqlDB.Stats()
		row.OpenConnections = stats.OpenConnections
		row.InUse = stats.InUse
		row.Idle = stats.Idle
		row.WaitCount = stats.WaitCount
		row.WaitDurationMS = int64(stats.WaitDuration / time.Millisecond)
		row.MaxOpenConnections = stats.MaxOpenConnections
		rows = append(rows, row)
	}
	return rows
}

func (p *Panel) systemFeatureFlags() []featureFlagState {
	if p == nil || p.flags == nil {
		return []featureFlagState{}
	}
	return p.flags.list()
}

func (p *Panel) defaultSystemDatabaseInfo() (DatabaseRuntimeInfo, bool) {
	dbInfos := p.config.Databases
	if len(dbInfos) == 0 {
		return DatabaseRuntimeInfo{}, false
	}
	for _, info := range dbInfos {
		if info.IsDefault {
			return info, true
		}
	}
	return dbInfos[0], true
}

func (p *Panel) lookupSystemDBHandle(info DatabaseRuntimeInfo) *db.DB {
	alias := strings.TrimSpace(info.Alias)
	if p.config.DatabaseHandles != nil && alias != "" {
		if handle, ok := p.config.DatabaseHandles[alias]; ok && handle != nil {
			return handle
		}
	}
	if info.IsDefault {
		return p.db
	}
	return nil
}

func parseSystemEnvLimit(r *http.Request, fallback int) int {
	if fallback <= 0 {
		fallback = defaultSystemEnvLimit
	}
	if r == nil {
		return fallback
	}
	raw := strings.TrimSpace(r.URL.Query().Get("env_limit"))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	if value > maxSystemEnvLimit {
		return maxSystemEnvLimit
	}
	return value
}

func summarizeOTLPEndpoint(raw string) string {
	endpoint := strings.TrimSpace(raw)
	if endpoint == "" {
		return ""
	}
	if !strings.Contains(endpoint, "://") {
		return endpoint
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || strings.TrimSpace(parsed.Host) == "" {
		return endpoint
	}
	scheme := strings.TrimSpace(parsed.Scheme)
	if scheme == "" {
		return parsed.Host
	}
	return scheme + "://" + parsed.Host
}

func outboxFlavorForDialect(raw string) outbox.Flavor {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "postgres", "postgresql":
		return outbox.FlavorPostgres
	case "mysql":
		return outbox.FlavorMySQL
	default:
		return outbox.FlavorSQLite
	}
}

func gatherGoroutineStateCounts() []systemStateCount {
	profile := pprof.Lookup("goroutine")
	if profile == nil {
		return []systemStateCount{{State: "unknown", Count: runtime.NumGoroutine()}}
	}

	var buf bytes.Buffer
	if err := profile.WriteTo(&buf, 1); err != nil {
		return []systemStateCount{{State: "unknown", Count: runtime.NumGoroutine()}}
	}

	states := map[string]int{}
	for _, line := range strings.Split(buf.String(), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "goroutine ") {
			continue
		}
		start := strings.Index(line, "[")
		end := strings.Index(line, "]")
		if start < 0 || end <= start+1 {
			continue
		}
		state := strings.TrimSpace(line[start+1 : end])
		if comma := strings.Index(state, ","); comma >= 0 {
			state = strings.TrimSpace(state[:comma])
		}
		if state == "" {
			state = "unknown"
		}
		states[state]++
	}
	if len(states) == 0 {
		states["unknown"] = runtime.NumGoroutine()
	}

	out := make([]systemStateCount, 0, len(states))
	for state, count := range states {
		out = append(out, systemStateCount{
			State: state,
			Count: count,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].State < out[j].State
	})
	return out
}

func lastGCPauseMS(mem runtime.MemStats) uint64 {
	if mem.NumGC == 0 {
		return 0
	}
	idx := (mem.NumGC - 1) % uint32(len(mem.PauseNs))
	return uint64(mem.PauseNs[idx] / uint64(time.Millisecond))
}

func buildSystemEnvironmentRows(entries []string) []systemEnvVar {
	rows := make([]systemEnvVar, 0, len(entries))
	for _, entry := range entries {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			key = entry
			value = ""
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		masked := shouldMaskSystemEnvValue(key)
		rows = append(rows, systemEnvVar{
			Name:   key,
			Value:  maskSystemEnvValue(value, masked),
			Masked: masked,
		})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].Name < rows[j].Name
	})
	return rows
}

func shouldMaskSystemEnvValue(name string) bool {
	upper := strings.ToUpper(strings.TrimSpace(name))
	return strings.Contains(upper, "KEY") ||
		strings.Contains(upper, "SECRET") ||
		strings.Contains(upper, "PASSWORD") ||
		strings.Contains(upper, "TOKEN")
}

func maskSystemEnvValue(value string, masked bool) string {
	if !masked {
		return value
	}
	return "***"
}

func getCPULoad() float64 {
	percents, err := cpu.Percent(0, false)
	if err == nil && len(percents) > 0 {
		return percents[0]
	}
	return 0
}

func getProcessCPULoad() float64 {
	p, err := process.NewProcess(int32(os.Getpid()))
	if err == nil {
		percent, err := p.Percent(0)
		if err == nil {
			return percent
		}
	}
	return 0
}
