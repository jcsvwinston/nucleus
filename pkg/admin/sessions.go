package admin

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jcsvwinston/GoFrame/pkg/auth"
)

const (
	defaultSessionListLimit = 250
	maxSessionListLimit     = 2000

	sessionRealtimeBucket = 10 * time.Second
	sessionRealtimeWindow = 10 * time.Minute

	sessionHourBucket = time.Minute
	sessionHourWindow = time.Hour

	sessionDayBucket = time.Hour
)

type sessionOverviewResponse struct {
	Enabled          bool             `json:"enabled"`
	Store            string           `json:"store"`
	Reason           string           `json:"reason,omitempty"`
	GeneratedAt      string           `json:"generated_at"`
	CurrentActive    int              `json:"current_active"`
	ActiveLast5Min   int              `json:"active_last_5m"`
	ActiveLastHour   int              `json:"active_last_hour"`
	Sessions         []sessionRow     `json:"sessions"`
	Telemetry        sessionTelemetry `json:"telemetry"`
	SourcePod        string           `json:"source_pod,omitempty"`
	SourceHost       string           `json:"source_host,omitempty"`
	SourceInstance   string           `json:"source_instance,omitempty"`
	IncludedRows     int              `json:"included_rows"`
	TruncatedByLimit bool             `json:"truncated_by_limit"`
}

type sessionRow struct {
	Token       string `json:"token"`
	TokenShort  string `json:"token_short"`
	User        string `json:"user,omitempty"`
	FirstSeenAt string `json:"first_seen_at,omitempty"`
	LastSeenAt  string `json:"last_seen_at,omitempty"`
	ExpiresAt   string `json:"expires_at,omitempty"`
	Pod         string `json:"pod,omitempty"`
	Host        string `json:"host,omitempty"`
	Instance    string `json:"instance,omitempty"`
	AgeSeconds  int64  `json:"age_seconds,omitempty"`
	IdleSeconds int64  `json:"idle_seconds,omitempty"`

	firstSeen time.Time
	lastSeen  time.Time
	expiresAt time.Time
}

type sessionTelemetry struct {
	Realtime sessionSeries `json:"realtime"`
	LastHour sessionSeries `json:"last_hour"`
	Today    sessionSeries `json:"today"`
}

type sessionSeries struct {
	Label         string               `json:"label"`
	BucketSeconds int                  `json:"bucket_seconds"`
	Points        []sessionSeriesPoint `json:"points"`
}

type sessionSeriesPoint struct {
	Timestamp string `json:"timestamp"`
	Active    int    `json:"active"`
}

type iterableSessionStore interface {
	All() (map[string][]byte, error)
}

type iterableSessionStoreCtx interface {
	AllCtx(context.Context) (map[string][]byte, error)
}

func (p *Panel) handleListSessions(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	limit := parseSessionListLimit(r, defaultSessionListLimit)

	resp := sessionOverviewResponse{
		Enabled:     false,
		Store:       normalizeSessionStoreLabel(p.config.SessionStore),
		GeneratedAt: now.Format(time.RFC3339),
		Telemetry: sessionTelemetry{
			Realtime: sessionSeries{
				Label:         "real_time",
				BucketSeconds: int(sessionRealtimeBucket.Seconds()),
				Points:        []sessionSeriesPoint{},
			},
			LastHour: sessionSeries{
				Label:         "last_hour",
				BucketSeconds: int(sessionHourBucket.Seconds()),
				Points:        []sessionSeriesPoint{},
			},
			Today: sessionSeries{
				Label:         "today",
				BucketSeconds: int(sessionDayBucket.Seconds()),
				Points:        []sessionSeriesPoint{},
			},
		},
	}

	if p.config.Session == nil {
		resp.Reason = "session manager is not configured in admin panel"
		writeJSON(w, http.StatusOK, resp)
		return
	}

	resp.Enabled = true
	resp.SourcePod = p.config.SessionRuntime.Pod
	resp.SourceHost = p.config.SessionRuntime.Host
	resp.SourceInstance = p.config.SessionRuntime.Instance

	rawSessions, supported, err := allSessionPayloads(r.Context(), p.config.Session)
	if err != nil {
		writeErr(w, fmt.Errorf("admin.ListSessions load: %w", err))
		return
	}
	if !supported {
		resp.Enabled = false
		resp.Reason = "session store does not support listing active sessions"
		writeJSON(w, http.StatusOK, resp)
		return
	}

	rows := make([]sessionRow, 0, len(rawSessions))
	for token, payload := range rawSessions {
		deadline, values, err := p.config.Session.SCS().Codec.Decode(payload)
		if err != nil {
			continue
		}

		row := buildSessionRow(token, deadline, values, now)
		rows = append(rows, row)
	}

	sort.SliceStable(rows, func(i, j int) bool {
		if !rows[i].lastSeen.Equal(rows[j].lastSeen) {
			return rows[i].lastSeen.After(rows[j].lastSeen)
		}
		if !rows[i].expiresAt.Equal(rows[j].expiresAt) {
			return rows[i].expiresAt.After(rows[j].expiresAt)
		}
		return rows[i].Token < rows[j].Token
	})

	resp.CurrentActive = len(rows)
	for _, row := range rows {
		if !row.lastSeen.IsZero() {
			if now.Sub(row.lastSeen) <= 5*time.Minute {
				resp.ActiveLast5Min++
			}
			if now.Sub(row.lastSeen) <= sessionHourWindow {
				resp.ActiveLastHour++
			}
		}
	}

	if len(rows) > limit {
		resp.TruncatedByLimit = true
		rows = rows[:limit]
	}
	resp.IncludedRows = len(rows)
	resp.Sessions = rows

	resp.Telemetry.Realtime.Points = buildSessionSeries(rows, now.Add(-sessionRealtimeWindow), now, sessionRealtimeBucket)
	resp.Telemetry.LastHour.Points = buildSessionSeries(rows, now.Add(-sessionHourWindow), now, sessionHourBucket)
	resp.Telemetry.Today.Points = buildTodaySeries(rows, now, sessionDayBucket)

	writeJSON(w, http.StatusOK, resp)
}

func buildSessionRow(token string, deadline time.Time, values map[string]interface{}, now time.Time) sessionRow {
	firstSeen := parseSessionMetaTime(valueAsString(values, auth.SessionMetaFirstSeenAtKey))
	lastSeen := parseSessionMetaTime(valueAsString(values, auth.SessionMetaLastSeenAtKey))
	if firstSeen.IsZero() {
		firstSeen = lastSeen
	}
	if lastSeen.IsZero() {
		lastSeen = firstSeen
	}
	if firstSeen.IsZero() {
		firstSeen = now
	}

	expiresAt := deadline.UTC()
	if expiresAt.IsZero() {
		expiresAt = now
	}

	row := sessionRow{
		Token:      token,
		TokenShort: shortenToken(token),
		User:       detectSessionUser(values),
		ExpiresAt:  formatIfSet(expiresAt),
		Pod:        valueAsString(values, auth.SessionMetaPodKey),
		Host:       valueAsString(values, auth.SessionMetaHostKey),
		Instance:   valueAsString(values, auth.SessionMetaInstanceKey),
		firstSeen:  firstSeen,
		lastSeen:   lastSeen,
		expiresAt:  expiresAt,
	}

	if !firstSeen.IsZero() {
		row.FirstSeenAt = firstSeen.Format(time.RFC3339)
		row.AgeSeconds = int64(now.Sub(firstSeen).Seconds())
	}
	if !lastSeen.IsZero() {
		row.LastSeenAt = lastSeen.Format(time.RFC3339)
		row.IdleSeconds = int64(now.Sub(lastSeen).Seconds())
	}

	return row
}

func parseSessionListLimit(r *http.Request, fallback int) int {
	if r == nil {
		return fallback
	}
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	if value > maxSessionListLimit {
		return maxSessionListLimit
	}
	return value
}

func buildSessionSeries(rows []sessionRow, start, end time.Time, bucket time.Duration) []sessionSeriesPoint {
	if bucket <= 0 {
		return []sessionSeriesPoint{}
	}
	if end.Before(start) {
		start, end = end, start
	}

	points := make([]sessionSeriesPoint, 0, int(end.Sub(start)/bucket)+1)
	for ts := start; !ts.After(end); ts = ts.Add(bucket) {
		active := 0
		for _, row := range rows {
			if isSessionActiveAt(row, ts) {
				active++
			}
		}
		points = append(points, sessionSeriesPoint{
			Timestamp: ts.Format(time.RFC3339),
			Active:    active,
		})
	}
	return points
}

func buildTodaySeries(rows []sessionRow, now time.Time, bucket time.Duration) []sessionSeriesPoint {
	loc := now.Location()
	startLocal := time.Date(now.In(loc).Year(), now.In(loc).Month(), now.In(loc).Day(), 0, 0, 0, 0, loc)
	return buildSessionSeries(rows, startLocal, now.In(loc), bucket)
}

func isSessionActiveAt(row sessionRow, ts time.Time) bool {
	start := row.firstSeen
	if start.IsZero() {
		start = row.lastSeen
	}
	if start.IsZero() {
		start = ts
	}
	end := row.expiresAt
	if end.IsZero() {
		end = ts.Add(time.Second)
	}

	return !start.After(ts) && end.After(ts)
}

func allSessionPayloads(ctx context.Context, sm *auth.SessionManager) (map[string][]byte, bool, error) {
	store := sm.SCS().Store

	if withCtx, ok := store.(iterableSessionStoreCtx); ok {
		all, err := withCtx.AllCtx(ctx)
		if err != nil {
			return nil, true, err
		}
		return all, true, nil
	}
	if plain, ok := store.(iterableSessionStore); ok {
		all, err := plain.All()
		if err != nil {
			return nil, true, err
		}
		return all, true, nil
	}
	return nil, false, nil
}

func parseSessionMetaTime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	ts, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}
	}
	return ts.UTC()
}

func valueAsString(values map[string]interface{}, key string) string {
	if len(values) == 0 || key == "" {
		return ""
	}
	raw, ok := values[key]
	if !ok || raw == nil {
		return ""
	}
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v)
	case []byte:
		return strings.TrimSpace(string(v))
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	}
}

func detectSessionUser(values map[string]interface{}) string {
	candidates := []string{
		"user_email",
		"email",
		"username",
		"user_name",
		"user_id",
		"uid",
		"id",
	}
	for _, key := range candidates {
		if v := valueAsString(values, key); v != "" {
			return v
		}
	}
	return ""
}

func shortenToken(token string) string {
	token = strings.TrimSpace(token)
	if len(token) <= 10 {
		return token
	}
	return token[:6] + "..." + token[len(token)-4:]
}

func formatIfSet(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.UTC().Format(time.RFC3339)
}

func normalizeSessionStoreLabel(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		return "memory"
	}
	return value
}
