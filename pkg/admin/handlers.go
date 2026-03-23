package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	gferrors "github.com/goframe/goframe/pkg/errors"
	"github.com/goframe/goframe/pkg/model"
)

// handleListModels returns all registered models with their record counts.
func (p *Panel) handleListModels(w http.ResponseWriter, r *http.Request) {
	type modelInfo struct {
		Name   string `json:"name"`
		Plural string `json:"plural"`
		Table  string `json:"table"`
		Icon   string `json:"icon"`
		Count  int64  `json:"count"`
	}

	models := p.registry.All()
	result := make([]modelInfo, 0, len(models))
	for _, m := range models {
		var count int64
		p.db.Table(m.Table).Where("deleted_at IS NULL").Count(&count)
		result = append(result, modelInfo{
			Name:   m.Name,
			Plural: m.Plural,
			Table:  m.Table,
			Icon:   m.Config.Icon,
			Count:  count,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"models": result,
		"title":  p.config.Title,
	})
}

// handleGetSchema returns metadata for a specific model.
func (p *Panel) handleGetSchema(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	meta, ok := p.registry.Get(name)
	if !ok {
		writeErr(w, gferrors.NotFound("model", name))
		return
	}

	type fieldInfo struct {
		Name         string          `json:"name"`
		Column       string          `json:"column"`
		Label        string          `json:"label"`
		Type         string          `json:"type"`
		HTMLType     string          `json:"html_type"`
		IsPK         bool            `json:"is_pk"`
		IsRequired   bool            `json:"is_required"`
		IsReadOnly   bool            `json:"is_readonly"`
		IsList       bool            `json:"is_list"`
		IsSearch     bool            `json:"is_search"`
		IsFilter     bool            `json:"is_filter"`
		IsExcluded   bool            `json:"is_excluded"`
		IsForeignKey bool            `json:"is_fk"`
		ForeignModel string          `json:"fk_model,omitempty"`
		Choices      []model.Choice  `json:"choices,omitempty"`
	}

	fields := make([]fieldInfo, 0, len(meta.Fields))
	for _, f := range meta.Fields {
		if f.IsExcluded {
			continue
		}
		fields = append(fields, fieldInfo{
			Name: f.Name, Column: f.Column, Label: f.Label,
			Type: f.GoType, HTMLType: f.HTMLType,
			IsPK: f.IsPK, IsRequired: f.IsRequired, IsReadOnly: f.IsReadOnly,
			IsList: f.IsList, IsSearch: f.IsSearch, IsFilter: f.IsFilter,
			IsExcluded: f.IsExcluded, IsForeignKey: f.IsForeignKey,
			ForeignModel: f.ForeignModel, Choices: f.Choices,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"name":       meta.Name,
		"plural":     meta.Plural,
		"table":      meta.Table,
		"primary_key": meta.PrimaryKey,
		"icon":       meta.Config.Icon,
		"read_only":  meta.Config.ReadOnly,
		"fields":     fields,
		"foreign_keys": meta.ForeignKeys,
	})
}

// handleListRecords returns a paginated list of records for a model.
func (p *Panel) handleListRecords(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	meta, ok := p.registry.Get(name)
	if !ok {
		writeErr(w, gferrors.NotFound("model", name))
		return
	}

	crud := p.getCRUD(meta)
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	search := r.URL.Query().Get("search")
	orderBy := r.URL.Query().Get("order_by")

	filters := make(map[string]string)
	for key, vals := range r.URL.Query() {
		if key != "page" && key != "page_size" && key != "search" && key != "order_by" {
			filters[key] = vals[0]
		}
	}

	result, err := crud.FindAll(r.Context(), model.QueryOpts{
		Page: page, PageSize: pageSize, Search: search,
		Filters: filters, OrderBy: orderBy,
	})
	if err != nil {
		writeErr(w, err)
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// handleGetRecord returns a single record by ID.
func (p *Panel) handleGetRecord(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	idStr := chi.URLParam(r, "id")

	meta, ok := p.registry.Get(name)
	if !ok {
		writeErr(w, gferrors.NotFound("model", name))
		return
	}

	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		writeErr(w, gferrors.BadRequest("invalid id"))
		return
	}

	crud := p.getCRUD(meta)
	record, err := crud.FindByID(r.Context(), uint(id))
	if err != nil {
		writeErr(w, err)
		return
	}

	writeJSON(w, http.StatusOK, record)
}

// handleCreateRecord creates a new record.
func (p *Panel) handleCreateRecord(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	meta, ok := p.registry.Get(name)
	if !ok {
		writeErr(w, gferrors.NotFound("model", name))
		return
	}
	if meta.Config.ReadOnly {
		writeErr(w, gferrors.Forbidden("model is read-only"))
		return
	}

	crud := p.getCRUD(meta)

	var data map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		writeErr(w, gferrors.BadRequest("invalid JSON: "+err.Error()))
		return
	}

	// Create via raw map insert for admin flexibility
	result := p.db.Table(meta.Table).Create(data)
	if result.Error != nil {
		writeErr(w, fmt.Errorf("admin.CreateRecord model=%s: %w", name, result.Error))
		return
	}

	_ = crud // for future hook support
	writeJSON(w, http.StatusCreated, data)
}

// handleUpdateRecord updates an existing record.
func (p *Panel) handleUpdateRecord(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	idStr := chi.URLParam(r, "id")

	meta, ok := p.registry.Get(name)
	if !ok {
		writeErr(w, gferrors.NotFound("model", name))
		return
	}
	if meta.Config.ReadOnly {
		writeErr(w, gferrors.Forbidden("model is read-only"))
		return
	}

	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		writeErr(w, gferrors.BadRequest("invalid id"))
		return
	}

	var updates map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		writeErr(w, gferrors.BadRequest("invalid JSON"))
		return
	}

	crud := p.getCRUD(meta)
	if err := crud.Update(r.Context(), uint(id), updates); err != nil {
		writeErr(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"updated": true, "id": id})
}

// handleDeleteRecord deletes a record by ID.
func (p *Panel) handleDeleteRecord(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	idStr := chi.URLParam(r, "id")

	meta, ok := p.registry.Get(name)
	if !ok {
		writeErr(w, gferrors.NotFound("model", name))
		return
	}
	if meta.Config.ReadOnly {
		writeErr(w, gferrors.Forbidden("model is read-only"))
		return
	}

	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		writeErr(w, gferrors.BadRequest("invalid id"))
		return
	}

	crud := p.getCRUD(meta)
	if err := crud.Delete(r.Context(), uint(id)); err != nil {
		writeErr(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"deleted": true, "id": id})
}

// handleBulkAction processes bulk operations (delete, export).
func (p *Panel) handleBulkAction(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	meta, ok := p.registry.Get(name)
	if !ok {
		writeErr(w, gferrors.NotFound("model", name))
		return
	}

	var req struct {
		Action string   `json:"action"`
		IDs    []uint   `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, gferrors.BadRequest("invalid JSON"))
		return
	}

	switch strings.ToLower(req.Action) {
	case "delete":
		if meta.Config.ReadOnly {
			writeErr(w, gferrors.Forbidden("model is read-only"))
			return
		}
		crud := p.getCRUD(meta)
		deleted := 0
		for _, id := range req.IDs {
			if err := crud.Delete(r.Context(), id); err == nil {
				deleted++
			}
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"deleted": deleted})

	default:
		writeErr(w, gferrors.BadRequest("unknown action: "+req.Action))
	}
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeErr(w http.ResponseWriter, err error) {
	gferrors.WriteError(w, err, nil)
}
