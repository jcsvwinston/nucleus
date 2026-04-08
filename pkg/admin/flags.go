package admin

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	gferrors "github.com/jcsvwinston/GoFrame/pkg/errors"
	"github.com/jcsvwinston/GoFrame/pkg/tasks"
)

type featureFlagStore struct {
	mu    sync.RWMutex
	flags map[string]featureFlagState
}

type featureFlagState struct {
	Name      string `json:"name"`
	Enabled   bool   `json:"enabled"`
	UpdatedAt string `json:"updated_at,omitempty"`
	UpdatedBy string `json:"updated_by,omitempty"`
}

func newFeatureFlagStore(initial map[string]bool) *featureFlagStore {
	store := &featureFlagStore{
		flags: make(map[string]featureFlagState),
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for name, enabled := range initial {
		normalized := normalizeFeatureFlagName(name)
		if normalized == "" {
			continue
		}
		store.flags[normalized] = featureFlagState{
			Name:      normalized,
			Enabled:   enabled,
			UpdatedAt: now,
			UpdatedBy: "bootstrap",
		}
	}
	return store
}

func normalizeFeatureFlagName(name string) string {
	normalized := strings.ToLower(strings.TrimSpace(name))
	normalized = strings.ReplaceAll(normalized, " ", "_")
	return normalized
}

func (s *featureFlagStore) list() []featureFlagState {
	if s == nil {
		return []featureFlagState{}
	}
	s.mu.RLock()
	rows := make([]featureFlagState, 0, len(s.flags))
	for _, item := range s.flags {
		rows = append(rows, item)
	}
	s.mu.RUnlock()
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].Name < rows[j].Name
	})
	return rows
}

func (s *featureFlagStore) get(name string) (featureFlagState, bool) {
	if s == nil {
		return featureFlagState{}, false
	}
	key := normalizeFeatureFlagName(name)
	if key == "" {
		return featureFlagState{}, false
	}
	s.mu.RLock()
	row, ok := s.flags[key]
	s.mu.RUnlock()
	return row, ok
}

func (s *featureFlagStore) set(name string, enabled bool, actor string) featureFlagState {
	key := normalizeFeatureFlagName(name)
	row := featureFlagState{
		Name:      key,
		Enabled:   enabled,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
		UpdatedBy: strings.TrimSpace(actor),
	}
	if row.UpdatedBy == "" {
		row.UpdatedBy = "admin"
	}
	if s == nil || key == "" {
		return row
	}
	s.mu.Lock()
	s.flags[key] = row
	s.mu.Unlock()
	return row
}

func (s *featureFlagStore) delete(name string) (featureFlagState, bool) {
	if s == nil {
		return featureFlagState{}, false
	}
	key := normalizeFeatureFlagName(name)
	if key == "" {
		return featureFlagState{}, false
	}
	s.mu.Lock()
	row, ok := s.flags[key]
	if ok {
		delete(s.flags, key)
	}
	s.mu.Unlock()
	return row, ok
}

// FeatureFlag returns one in-memory feature flag value.
func (p *Panel) FeatureFlag(name string) (enabled bool, ok bool) {
	if p == nil || p.flags == nil {
		return false, false
	}
	row, exists := p.flags.get(name)
	return row.Enabled, exists
}

// SetFeatureFlag upserts one in-memory feature flag value.
func (p *Panel) SetFeatureFlag(name string, enabled bool) {
	if p == nil || p.flags == nil {
		return
	}
	p.flags.set(name, enabled, "runtime")
}

func (p *Panel) handleListSystemFlags(w http.ResponseWriter, r *http.Request) {
	if !p.authorizeAction(w, r, "*", "system_pulse") {
		return
	}
	rows := []featureFlagState{}
	if p != nil && p.flags != nil {
		rows = p.flags.list()
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"enabled": true,
		"count":   len(rows),
		"flags":   rows,
	})
}

func (p *Panel) handleSetSystemFlag(w http.ResponseWriter, r *http.Request) {
	if !p.authorizeAction(w, r, "*", "feature_flags_write") {
		return
	}
	if p == nil || p.flags == nil {
		writeErr(w, gferrors.BadRequest("feature flags store is not available"))
		return
	}

	name := normalizeFeatureFlagName(r.PathValue("name"))
	if name == "" {
		writeErr(w, gferrors.BadRequest("feature flag name is required"))
		return
	}

	var payload struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeErr(w, gferrors.BadRequest("invalid JSON"))
		return
	}

	row := p.flags.set(name, payload.Enabled, p.runtimeActor(r))
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"updated": true,
		"flag":    row,
	})
}

func (p *Panel) handleCreateSystemFlag(w http.ResponseWriter, r *http.Request) {
	if !p.authorizeAction(w, r, "*", "feature_flags_write") {
		return
	}
	if p == nil || p.flags == nil {
		writeErr(w, gferrors.BadRequest("feature flags store is not available"))
		return
	}

	var payload struct {
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeErr(w, gferrors.BadRequest("invalid JSON"))
		return
	}

	name := normalizeFeatureFlagName(payload.Name)
	if name == "" {
		writeErr(w, gferrors.BadRequest("feature flag name is required"))
		return
	}

	_, existed := p.flags.get(name)
	row := p.flags.set(name, payload.Enabled, p.runtimeActor(r))
	status := http.StatusCreated
	if existed {
		status = http.StatusOK
	}
	writeJSON(w, status, map[string]interface{}{
		"created": !existed,
		"flag":    row,
	})
}

func (p *Panel) handleDeleteSystemFlag(w http.ResponseWriter, r *http.Request) {
	if !p.authorizeAction(w, r, "*", "feature_flags_write") {
		return
	}
	if p == nil || p.flags == nil {
		writeErr(w, gferrors.BadRequest("feature flags store is not available"))
		return
	}

	name := normalizeFeatureFlagName(r.PathValue("name"))
	if name == "" {
		writeErr(w, gferrors.BadRequest("feature flag name is required"))
		return
	}

	row, ok := p.flags.delete(name)
	if !ok {
		writeErr(w, gferrors.NotFound("feature flag", name))
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"deleted": true,
		"flag":    row,
	})
}

func (p *Panel) runtimeActor(r *http.Request) string {
	actor := "admin"
	if p == nil || p.config.Auth == nil {
		return actor
	}
	if user, err := p.authenticatedUser(r); err == nil && user != nil {
		if trimmed := strings.TrimSpace(user.ID); trimmed != "" {
			actor = trimmed
		}
	}
	return actor
}

const runtimeQueueOperationAck = "I_UNDERSTAND_RUNTIME_OPERATION"

func (p *Panel) handleSystemQueueAction(w http.ResponseWriter, r *http.Request) {
	if !p.authorizeAction(w, r, "*", "system_jobs_write") {
		return
	}

	queue := strings.TrimSpace(r.PathValue("name"))
	action := strings.ToLower(strings.TrimSpace(r.PathValue("action")))
	if queue == "" {
		writeErr(w, gferrors.BadRequest("queue is required"))
		return
	}
	switch action {
	case "pause", "unpause", "retry":
	default:
		writeErr(w, gferrors.BadRequest("unsupported queue action"))
		return
	}

	var payload struct {
		ConfirmQueue string `json:"confirm_queue"`
		Acknowledge  string `json:"acknowledge"`
		Force        bool   `json:"force"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeErr(w, gferrors.BadRequest("invalid JSON"))
		return
	}
	if strings.TrimSpace(payload.ConfirmQueue) != queue {
		writeErr(w, gferrors.BadRequest("confirm_queue must match queue name"))
		return
	}
	if strings.TrimSpace(payload.Acknowledge) != runtimeQueueOperationAck {
		writeErr(w, gferrors.BadRequest("runtime operation acknowledgment is required"))
		return
	}

	if strings.EqualFold(strings.TrimSpace(p.config.Environment), "production") && !payload.Force {
		writeErr(w, gferrors.Forbidden("runtime queue operations in production require force=true"))
		return
	}

	result, err := tasks.OperateQueue(p.config.RedisURL, queue, action)
	if err != nil {
		errText := strings.ToLower(strings.TrimSpace(err.Error()))
		if strings.Contains(errText, "required") || strings.Contains(errText, "unsupported") || strings.Contains(errText, "invalid") {
			writeErr(w, gferrors.BadRequest(err.Error()))
			return
		}
		writeErr(w, gferrors.InternalError("queue operation failed").WithDetails(map[string]interface{}{
			"queue":  queue,
			"action": action,
		}))
		return
	}
	writeJSON(w, http.StatusOK, result)
}
