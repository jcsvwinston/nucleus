package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jcsvwinston/GoFrame/pkg/db"
	gferrors "github.com/jcsvwinston/GoFrame/pkg/errors"
	"github.com/jcsvwinston/GoFrame/pkg/model"
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
		count, err := p.modelCount(r.Context(), m)
		if err != nil {
			writeErr(w, fmt.Errorf("admin.ListModels count model=%s: %w", m.Name, err))
			return
		}
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
		Name         string         `json:"name"`
		Column       string         `json:"column"`
		Label        string         `json:"label"`
		Type         string         `json:"type"`
		HTMLType     string         `json:"html_type"`
		IsPK         bool           `json:"is_pk"`
		IsRequired   bool           `json:"is_required"`
		IsReadOnly   bool           `json:"is_readonly"`
		IsList       bool           `json:"is_list"`
		IsSearch     bool           `json:"is_search"`
		IsFilter     bool           `json:"is_filter"`
		IsExcluded   bool           `json:"is_excluded"`
		IsForeignKey bool           `json:"is_fk"`
		ForeignModel string         `json:"fk_model,omitempty"`
		Choices      []model.Choice `json:"choices,omitempty"`
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
		"name":         meta.Name,
		"plural":       meta.Plural,
		"table":        meta.Table,
		"primary_key":  meta.PrimaryKey,
		"icon":         meta.Config.Icon,
		"read_only":    meta.Config.ReadOnly,
		"fields":       fields,
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

	crud, err := p.getCRUD(meta)
	if err != nil {
		writeErr(w, err)
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	search := r.URL.Query().Get("search")
	orderBy, err := sanitizeOrderBy(meta, r.URL.Query().Get("order_by"))
	if err != nil {
		writeErr(w, err)
		return
	}

	filters := make(map[string]string)
	for key, vals := range r.URL.Query() {
		if key != "page" && key != "page_size" && key != "search" && key != "order_by" {
			raw := strings.TrimSpace(vals[0])
			if raw == "" {
				continue
			}
			col, normalized, ok := normalizeFilter(meta, key, raw)
			if !ok {
				continue
			}
			filters[col] = normalized
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

	crud, err := p.getCRUD(meta)
	if err != nil {
		writeErr(w, err)
		return
	}
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

	crud, err := p.getCRUD(meta)
	if err != nil {
		writeErr(w, err)
		return
	}

	var data map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		writeErr(w, gferrors.BadRequest("invalid JSON: "+err.Error()))
		return
	}

	entity, err := payloadToEntity(meta, data)
	if err != nil {
		writeErr(w, err)
		return
	}

	if err := crud.Create(r.Context(), entity); err != nil {
		writeErr(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, entity)
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

	crud, err := p.getCRUD(meta)
	if err != nil {
		writeErr(w, err)
		return
	}
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

	crud, err := p.getCRUD(meta)
	if err != nil {
		writeErr(w, err)
		return
	}
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
		Action string `json:"action"`
		IDs    []uint `json:"ids"`
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
		crud, err := p.getCRUD(meta)
		if err != nil {
			writeErr(w, err)
			return
		}
		deleted := 0
		for _, id := range req.IDs {
			if err := crud.Delete(r.Context(), id); err == nil {
				deleted++
			}
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"deleted": deleted})

	case "export":
		if len(req.IDs) == 0 {
			writeErr(w, gferrors.BadRequest("ids are required for export action"))
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"export_url": buildBulkExportURL(r.URL.Path, req.IDs),
			"ids":        req.IDs,
		})

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

func (p *Panel) modelCount(ctx context.Context, meta *model.ModelMeta) (int64, error) {
	if p.db == nil {
		return 0, fmt.Errorf("nil database")
	}

	switch p.db.Engine() {
	case db.EngineBun:
		bunDB := p.db.BunDB()
		if bunDB == nil {
			return 0, db.ErrBunRequired
		}
		query := bunDB.NewSelect().Table(meta.Table)
		if hasDeletedAt(meta) {
			query = query.Where("deleted_at IS NULL")
		}
		count, err := query.Count(ctx)
		if err != nil {
			return 0, err
		}
		return int64(count), nil

	case db.EngineGORM:
		gormDB := p.db.GormDB()
		if gormDB == nil {
			return 0, db.ErrGORMRequired
		}
		query := gormDB.Table(meta.Table)
		if hasDeletedAt(meta) {
			query = query.Where("deleted_at IS NULL")
		}
		var count int64
		if err := query.Count(&count).Error; err != nil {
			return 0, err
		}
		return count, nil
	default:
		return 0, fmt.Errorf("unsupported engine %s", p.db.Engine())
	}
}

func hasDeletedAt(meta *model.ModelMeta) bool {
	for _, f := range meta.Fields {
		if f.Column == "deleted_at" {
			return true
		}
	}
	return false
}

func payloadToEntity(meta *model.ModelMeta, data map[string]interface{}) (interface{}, error) {
	entityPtr := reflect.New(meta.Type)
	entity := entityPtr.Elem()

	for key, raw := range data {
		fieldMeta, ok := fieldForInput(meta, key)
		if !ok || fieldMeta.IsPK || fieldMeta.IsReadOnly {
			continue
		}

		field := entity.FieldByName(fieldMeta.Name)
		if !field.IsValid() || !field.CanSet() {
			continue
		}

		if err := assignInputValue(field, raw); err != nil {
			return nil, gferrors.BadRequest(fmt.Sprintf("invalid value for %s", key))
		}
	}

	return entityPtr.Interface(), nil
}

func fieldForInput(meta *model.ModelMeta, key string) (model.FieldMeta, bool) {
	for _, f := range meta.Fields {
		if strings.EqualFold(key, f.Column) || strings.EqualFold(key, f.Name) {
			return f, true
		}
	}
	return model.FieldMeta{}, false
}

func assignInputValue(field reflect.Value, raw interface{}) error {
	if raw == nil {
		return nil
	}

	fieldType := field.Type()
	if fieldType.Kind() == reflect.Ptr {
		ptr := reflect.New(fieldType.Elem())
		if err := assignInputValue(ptr.Elem(), raw); err != nil {
			return err
		}
		field.Set(ptr)
		return nil
	}

	if isTimeType(fieldType) {
		ts, err := parseTimeValue(raw)
		if err != nil {
			return err
		}
		field.Set(reflect.ValueOf(ts))
		return nil
	}

	switch field.Kind() {
	case reflect.String:
		field.SetString(fmt.Sprintf("%v", raw))
		return nil

	case reflect.Bool:
		v, ok := raw.(bool)
		if ok {
			field.SetBool(v)
			return nil
		}
		s := strings.ToLower(fmt.Sprintf("%v", raw))
		field.SetBool(s == "1" || s == "true" || s == "yes" || s == "on")
		return nil

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := toInt64(raw)
		if err != nil {
			return err
		}
		field.SetInt(n)
		return nil

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := toUint64(raw)
		if err != nil {
			return err
		}
		field.SetUint(n)
		return nil

	case reflect.Float32, reflect.Float64:
		f, err := toFloat64(raw)
		if err != nil {
			return err
		}
		field.SetFloat(f)
		return nil
	}

	val := reflect.ValueOf(raw)
	if val.Type().AssignableTo(fieldType) {
		field.Set(val)
		return nil
	}
	if val.Type().ConvertibleTo(fieldType) {
		field.Set(val.Convert(fieldType))
		return nil
	}
	return fmt.Errorf("unsupported conversion")
}

func isTimeType(t reflect.Type) bool {
	return t.PkgPath() == "time" && t.Name() == "Time"
}

func parseTimeValue(raw interface{}) (time.Time, error) {
	switch v := raw.(type) {
	case time.Time:
		return v, nil
	case string:
		v = strings.TrimSpace(v)
		if v == "" {
			return time.Time{}, nil
		}
		layouts := []string{
			time.RFC3339,
			"2006-01-02T15:04",
			"2006-01-02 15:04:05",
			"2006-01-02",
		}
		for _, layout := range layouts {
			if ts, err := time.Parse(layout, v); err == nil {
				return ts, nil
			}
		}
	}
	return time.Time{}, fmt.Errorf("invalid time value")
}

func toInt64(raw interface{}) (int64, error) {
	switch v := raw.(type) {
	case float64:
		return int64(v), nil
	case float32:
		return int64(v), nil
	case int:
		return int64(v), nil
	case int8:
		return int64(v), nil
	case int16:
		return int64(v), nil
	case int32:
		return int64(v), nil
	case int64:
		return v, nil
	case uint:
		return int64(v), nil
	case uint8:
		return int64(v), nil
	case uint16:
		return int64(v), nil
	case uint32:
		return int64(v), nil
	case uint64:
		return int64(v), nil
	case string:
		return strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	default:
		return strconv.ParseInt(strings.TrimSpace(fmt.Sprintf("%v", raw)), 10, 64)
	}
}

func toUint64(raw interface{}) (uint64, error) {
	switch v := raw.(type) {
	case float64:
		return uint64(v), nil
	case float32:
		return uint64(v), nil
	case int:
		return uint64(v), nil
	case int8:
		return uint64(v), nil
	case int16:
		return uint64(v), nil
	case int32:
		return uint64(v), nil
	case int64:
		return uint64(v), nil
	case uint:
		return uint64(v), nil
	case uint8:
		return uint64(v), nil
	case uint16:
		return uint64(v), nil
	case uint32:
		return uint64(v), nil
	case uint64:
		return v, nil
	case string:
		return strconv.ParseUint(strings.TrimSpace(v), 10, 64)
	default:
		return strconv.ParseUint(strings.TrimSpace(fmt.Sprintf("%v", raw)), 10, 64)
	}
}

func toFloat64(raw interface{}) (float64, error) {
	switch v := raw.(type) {
	case float64:
		return v, nil
	case float32:
		return float64(v), nil
	case int:
		return float64(v), nil
	case int8:
		return float64(v), nil
	case int16:
		return float64(v), nil
	case int32:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case uint:
		return float64(v), nil
	case uint8:
		return float64(v), nil
	case uint16:
		return float64(v), nil
	case uint32:
		return float64(v), nil
	case uint64:
		return float64(v), nil
	case string:
		return strconv.ParseFloat(strings.TrimSpace(v), 64)
	default:
		return strconv.ParseFloat(strings.TrimSpace(fmt.Sprintf("%v", raw)), 64)
	}
}

func sanitizeOrderBy(meta *model.ModelMeta, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}

	parts := strings.Fields(raw)
	if len(parts) == 0 || len(parts) > 2 {
		return "", gferrors.BadRequest("invalid order_by")
	}

	col, _, ok := resolveField(meta, parts[0])
	if !ok {
		return "", gferrors.BadRequest("invalid order_by column")
	}

	dir := "asc"
	if len(parts) == 2 {
		d := strings.ToLower(parts[1])
		if d != "asc" && d != "desc" {
			return "", gferrors.BadRequest("invalid order_by direction")
		}
		dir = d
	}

	return fmt.Sprintf("%s %s", col, dir), nil
}

func normalizeFilter(meta *model.ModelMeta, key, value string) (column, normalized string, ok bool) {
	col, field, found := resolveField(meta, key)
	if !found {
		return "", "", false
	}

	normalized = value
	if strings.EqualFold(field.GoType, "bool") {
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "1", "true", "yes", "on":
			normalized = "1"
		case "0", "false", "no", "off":
			normalized = "0"
		default:
			return "", "", false
		}
	}

	return col, normalized, true
}

func resolveField(meta *model.ModelMeta, key string) (column string, field model.FieldMeta, ok bool) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", model.FieldMeta{}, false
	}

	for _, f := range meta.Fields {
		col := runtimeColumn(f.Column)
		if strings.EqualFold(key, col) || strings.EqualFold(key, f.Column) || strings.EqualFold(key, f.Name) {
			return col, f, true
		}
	}
	return "", model.FieldMeta{}, false
}

func runtimeColumn(col string) string {
	if col == "i_d" {
		return "id"
	}
	return col
}

func buildBulkExportURL(currentPath string, ids []uint) string {
	base := strings.TrimSuffix(currentPath, "/bulk")
	if base == currentPath {
		base = strings.TrimSuffix(currentPath, "/")
	}

	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, strconv.FormatUint(uint64(id), 10))
	}

	q := url.Values{}
	q.Set("ids", strings.Join(parts, ","))
	return base + "/export?" + q.Encode()
}
