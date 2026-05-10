// Package datastudio is the agent-side handler for DataStudioRequest
// frames sent by the admin server. The admin server has no direct DB
// access; it routes UI Data Studio operations to a connected agent
// over the existing bidi stream. The agent executes the operation
// locally via pkg/model.CRUD (preserving signals, validation,
// multi-tenant resolution, RBAC) and sends a DataStudioResponse back.
//
// Construction is opt-in: pass a non-nil *model.Registry and at least
// one *db.DB in the agent's Config. When the registry is nil, the
// Handler is disabled and the agent ignores DataStudioRequests with a
// canned "data studio not enabled on this agent" error response.
package datastudio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/db"
	"github.com/jcsvwinston/nucleus/pkg/model"

	adminv1 "github.com/jcsvwinston/nucleus/admin/proto/gen/go/nucleus/admin/v1"
)

// Config wires the handler into the framework's existing CRUD plumbing.
type Config struct {
	// Registry is the model registry to introspect. Nil disables the
	// handler.
	Registry *model.Registry

	// Databases is the alias -> DB handle map. Must contain at least
	// the DefaultAlias when the handler is enabled.
	Databases map[string]*db.DB

	// DefaultAlias is used when a request's database_alias is empty.
	// Defaults to "default".
	DefaultAlias string
}

func (c Config) defaultAlias() string {
	if a := strings.TrimSpace(c.DefaultAlias); a != "" {
		return a
	}
	return "default"
}

// Handler dispatches DataStudioRequest frames. It is goroutine-safe;
// the agent's stream layer calls Dispatch from its receive loop.
type Handler struct {
	cfg Config

	// crudCache is keyed by "<alias>::<modelName>"; mu protects access.
	mu        sync.Mutex
	crudCache map[string]*model.CRUD
}

// New constructs a Handler. Returns nil when cfg.Registry is nil
// (caller treats this as "Data Studio disabled on this agent").
func New(cfg Config) *Handler {
	if cfg.Registry == nil {
		return nil
	}
	return &Handler{
		cfg:       cfg,
		crudCache: make(map[string]*model.CRUD),
	}
}

// RegisteredModels returns the names of every model this handler can
// serve. The agent ships this list in NodeRegistration so the admin
// server can route requests to the right node.
func (h *Handler) RegisteredModels() []string {
	if h == nil || h.cfg.Registry == nil {
		return nil
	}
	all := h.cfg.Registry.All()
	out := make([]string, 0, len(all))
	for _, m := range all {
		out = append(out, m.Name)
	}
	return out
}

// Dispatch executes the request and returns the response. The response
// always carries the same RequestId; on failure, Error is non-empty
// and Body is nil. Dispatch never returns nil.
func (h *Handler) Dispatch(ctx context.Context, req *adminv1.DataStudioRequest) *adminv1.DataStudioResponse {
	resp := &adminv1.DataStudioResponse{RequestId: req.GetRequestId()}

	if h == nil || h.cfg.Registry == nil {
		resp.Error = "admin agent: data studio is not enabled on this node"
		return resp
	}

	switch body := req.GetBody().(type) {
	case *adminv1.DataStudioRequest_ListModels:
		h.handleListModels(resp, body.ListModels)
	case *adminv1.DataStudioRequest_GetSchema:
		h.handleGetSchema(resp, body.GetSchema)
	case *adminv1.DataStudioRequest_ListRecords:
		h.handleListRecords(ctx, resp, body.ListRecords)
	case *adminv1.DataStudioRequest_GetRecord:
		h.handleGetRecord(ctx, resp, body.GetRecord)
	case *adminv1.DataStudioRequest_CreateRecord:
		h.handleCreateRecord(ctx, resp, body.CreateRecord)
	case *adminv1.DataStudioRequest_UpdateRecord:
		h.handleUpdateRecord(ctx, resp, body.UpdateRecord)
	case *adminv1.DataStudioRequest_DeleteRecord:
		h.handleDeleteRecord(ctx, resp, body.DeleteRecord)
	case *adminv1.DataStudioRequest_BulkAction:
		h.handleBulkAction(ctx, resp, body.BulkAction)
	default:
		resp.Error = fmt.Sprintf("admin agent: unsupported data studio request: %T", body)
	}
	return resp
}

// =============================================================================
// list_models / get_schema (read-only metadata)
// =============================================================================

func (h *Handler) handleListModels(resp *adminv1.DataStudioResponse, req *adminv1.ListModelsRequest) {
	all := h.cfg.Registry.All()
	out := make([]*adminv1.ModelInfo, 0, len(all))
	alias := strings.TrimSpace(req.GetDatabaseAlias())

	for _, m := range all {
		if alias != "" && m.DatabaseAlias != "" && !strings.EqualFold(alias, m.DatabaseAlias) {
			continue
		}
		info := h.metaToInfo(m)
		if req.GetIncludeCounts() {
			if c, ok := h.crudFor(m, alias); ok {
				if total, estimated, err := h.countModel(c); err == nil {
					info.RecordCount = total
					info.RecordCountEstimated = estimated
				} else {
					info.RecordCount = -1
				}
			}
		} else {
			info.RecordCount = -1
		}
		out = append(out, info)
	}
	resp.Body = &adminv1.DataStudioResponse_ListModels{
		ListModels: &adminv1.ListModelsResponse{Models: out},
	}
}

func (h *Handler) handleGetSchema(resp *adminv1.DataStudioResponse, req *adminv1.GetSchemaRequest) {
	meta, ok := h.cfg.Registry.Get(req.GetModelName())
	if !ok {
		resp.Error = fmt.Sprintf("admin agent: model %q is not registered", req.GetModelName())
		return
	}
	resp.Body = &adminv1.DataStudioResponse_Schema{
		Schema: &adminv1.ModelSchema{
			Info:   h.metaToInfo(meta),
			Fields: fieldsToProto(meta.Fields),
		},
	}
}

// =============================================================================
// list_records / get_record
// =============================================================================

func (h *Handler) handleListRecords(ctx context.Context, resp *adminv1.DataStudioResponse, req *adminv1.ListRecordsRequest) {
	meta, ok := h.cfg.Registry.Get(req.GetModelName())
	if !ok {
		resp.Error = fmt.Sprintf("admin agent: model %q is not registered", req.GetModelName())
		return
	}
	c, ok := h.crudFor(meta, req.GetDatabaseAlias())
	if !ok {
		resp.Error = fmt.Sprintf("admin agent: database alias not configured for %q", req.GetModelName())
		return
	}

	page := int(req.GetPage())
	if page < 1 {
		page = 1
	}
	pageSize := int(req.GetPageSize())
	if pageSize < 1 {
		pageSize = 25
	}

	opts := model.QueryOpts{
		Page:     page,
		PageSize: pageSize,
		Search:   req.GetSearch(),
		OrderBy:  req.GetOrderBy(),
		Filters:  req.GetFilters(),
		Fields:   req.GetFields(),
	}

	result, err := c.FindAll(ctx, opts)
	if err != nil {
		resp.Error = err.Error()
		return
	}

	items := entitiesToRecords(result.Items, meta)

	resp.Body = &adminv1.DataStudioResponse_RecordsPage{
		RecordsPage: &adminv1.PaginatedRecords{
			Items:          items,
			Page:           uint32(result.Page),
			PageSize:       uint32(result.PageSize),
			Total:          result.Total,
			TotalEstimated: result.IsEstimated,
			HasMore:        result.HasMore,
		},
	}
}

func (h *Handler) handleGetRecord(ctx context.Context, resp *adminv1.DataStudioResponse, req *adminv1.GetRecordRequest) {
	meta, ok := h.cfg.Registry.Get(req.GetModelName())
	if !ok {
		resp.Error = fmt.Sprintf("admin agent: model %q is not registered", req.GetModelName())
		return
	}
	c, ok := h.crudFor(meta, req.GetDatabaseAlias())
	if !ok {
		resp.Error = fmt.Sprintf("admin agent: database alias not configured for %q", req.GetModelName())
		return
	}
	pk, err := parseID(req.GetId(), meta)
	if err != nil {
		resp.Error = err.Error()
		return
	}
	entity, err := c.FindByID(ctx, pk)
	if err != nil {
		resp.Error = err.Error()
		return
	}
	resp.Body = &adminv1.DataStudioResponse_Record{
		Record: entityToRecord(entity, meta),
	}
}

// =============================================================================
// create / update / delete / bulk
// =============================================================================

func (h *Handler) handleCreateRecord(ctx context.Context, resp *adminv1.DataStudioResponse, req *adminv1.CreateRecordRequest) {
	meta, ok := h.cfg.Registry.Get(req.GetModelName())
	if !ok {
		resp.Error = fmt.Sprintf("admin agent: model %q is not registered", req.GetModelName())
		return
	}
	c, ok := h.crudFor(meta, req.GetDatabaseAlias())
	if !ok {
		resp.Error = fmt.Sprintf("admin agent: database alias not configured for %q", req.GetModelName())
		return
	}
	entity, err := buildEntityFromRecord(req.GetRecord(), meta)
	if err != nil {
		resp.Error = err.Error()
		return
	}
	if err := c.Create(ctx, entity); err != nil {
		resp.Error = err.Error()
		return
	}
	resp.Body = &adminv1.DataStudioResponse_Record{Record: entityToRecord(entity, meta)}
}

func (h *Handler) handleUpdateRecord(ctx context.Context, resp *adminv1.DataStudioResponse, req *adminv1.UpdateRecordRequest) {
	meta, ok := h.cfg.Registry.Get(req.GetModelName())
	if !ok {
		resp.Error = fmt.Sprintf("admin agent: model %q is not registered", req.GetModelName())
		return
	}
	c, ok := h.crudFor(meta, req.GetDatabaseAlias())
	if !ok {
		resp.Error = fmt.Sprintf("admin agent: database alias not configured for %q", req.GetModelName())
		return
	}
	pk, err := parseID(req.GetId(), meta)
	if err != nil {
		resp.Error = err.Error()
		return
	}
	updates, err := recordToUpdates(req.GetRecord(), meta)
	if err != nil {
		resp.Error = err.Error()
		return
	}
	if err := c.Update(ctx, pk, updates); err != nil {
		resp.Error = err.Error()
		return
	}
	updated, err := c.FindByID(ctx, pk)
	if err != nil {
		resp.Error = err.Error()
		return
	}
	resp.Body = &adminv1.DataStudioResponse_Record{Record: entityToRecord(updated, meta)}
}

func (h *Handler) handleDeleteRecord(ctx context.Context, resp *adminv1.DataStudioResponse, req *adminv1.DeleteRecordRequest) {
	meta, ok := h.cfg.Registry.Get(req.GetModelName())
	if !ok {
		resp.Error = fmt.Sprintf("admin agent: model %q is not registered", req.GetModelName())
		return
	}
	c, ok := h.crudFor(meta, req.GetDatabaseAlias())
	if !ok {
		resp.Error = fmt.Sprintf("admin agent: database alias not configured for %q", req.GetModelName())
		return
	}
	pk, err := parseID(req.GetId(), meta)
	if err != nil {
		resp.Error = err.Error()
		return
	}
	if err := c.Delete(ctx, pk); err != nil {
		resp.Error = err.Error()
		return
	}
	resp.Body = &adminv1.DataStudioResponse_DeleteRecord{
		DeleteRecord: &adminv1.DeleteRecordResponse{Deleted: true},
	}
}

func (h *Handler) handleBulkAction(ctx context.Context, resp *adminv1.DataStudioResponse, req *adminv1.BulkActionRequest) {
	meta, ok := h.cfg.Registry.Get(req.GetModelName())
	if !ok {
		resp.Error = fmt.Sprintf("admin agent: model %q is not registered", req.GetModelName())
		return
	}
	c, ok := h.crudFor(meta, req.GetDatabaseAlias())
	if !ok {
		resp.Error = fmt.Sprintf("admin agent: database alias not configured for %q", req.GetModelName())
		return
	}

	switch strings.ToLower(strings.TrimSpace(req.GetAction())) {
	case "delete":
		out := &adminv1.BulkActionResponse{}
		for _, raw := range req.GetIds() {
			pk, err := parseID(raw, meta)
			if err != nil {
				out.Failed++
				out.Errors = append(out.Errors, fmt.Sprintf("%s: %v", raw, err))
				continue
			}
			if err := c.Delete(ctx, pk); err != nil {
				out.Failed++
				out.Errors = append(out.Errors, fmt.Sprintf("%s: %v", raw, err))
				continue
			}
			out.Affected++
		}
		resp.Body = &adminv1.DataStudioResponse_BulkAction{BulkAction: out}
	default:
		resp.Error = fmt.Sprintf("admin agent: unsupported bulk action %q", req.GetAction())
	}
}

// =============================================================================
// helpers
// =============================================================================

func (h *Handler) crudFor(meta *model.ModelMeta, alias string) (*model.CRUD, bool) {
	a := strings.TrimSpace(alias)
	if a == "" {
		a = strings.TrimSpace(meta.DatabaseAlias)
	}
	if a == "" {
		a = h.cfg.defaultAlias()
	}

	key := a + "::" + meta.Name
	h.mu.Lock()
	defer h.mu.Unlock()
	if c, ok := h.crudCache[key]; ok {
		return c, true
	}

	dbHandle, ok := h.cfg.Databases[a]
	if !ok || dbHandle == nil {
		return nil, false
	}
	sqlDB, err := dbHandle.SqlDB()
	if err != nil {
		return nil, false
	}
	c := model.NewCRUD(sqlDB, meta, nil)
	h.crudCache[key] = c
	return c, true
}

func (h *Handler) countModel(c *model.CRUD) (int64, bool, error) {
	// FindAll with a tiny page returns Total + IsEstimated honestly;
	// re-using it avoids duplicating the dialect/estimate logic.
	res, err := c.FindAll(context.Background(), model.QueryOpts{Page: 1, PageSize: 1})
	if err != nil {
		return 0, false, err
	}
	return res.Total, res.IsEstimated, nil
}

func (h *Handler) metaToInfo(m *model.ModelMeta) *adminv1.ModelInfo {
	return &adminv1.ModelInfo{
		Name:          m.Name,
		Plural:        m.Plural,
		Table:         m.Table,
		DatabaseAlias: m.DatabaseAlias,
		PrimaryKey:    m.PrimaryKey,
		RecordCount:   -1,
	}
}

func fieldsToProto(in []model.FieldMeta) []*adminv1.ModelField {
	out := make([]*adminv1.ModelField, 0, len(in))
	for _, f := range in {
		out = append(out, &adminv1.ModelField{
			Name:         f.Name,
			Column:       f.Column,
			Label:        f.Label,
			GoType:       f.GoType,
			HtmlType:     f.HTMLType,
			IsPrimaryKey: f.IsPK,
			IsRequired:   f.IsRequired,
			IsReadonly:   f.IsReadOnly,
			IsInList:     f.IsList,
			IsSearchable: f.IsSearch,
			IsFilterable: f.IsFilter,
			IsExcluded:   f.IsExcluded,
			IsForeignKey: f.IsForeignKey,
			ForeignModel: f.ForeignModel,
			MaxLength:    int32(f.MaxLength),
			Choices:      choicesToProto(f.Choices),
		})
	}
	return out
}

func choicesToProto(in []model.Choice) []*adminv1.FieldChoice {
	if len(in) == 0 {
		return nil
	}
	out := make([]*adminv1.FieldChoice, 0, len(in))
	for _, c := range in {
		out = append(out, &adminv1.FieldChoice{Value: c.Value, Label: c.Label})
	}
	return out
}

// entitiesToRecords accepts a reflect.SliceOf(meta.Type) and serializes
// each element to a Record by JSON-encoding every non-excluded field.
func entitiesToRecords(itemsAny any, meta *model.ModelMeta) []*adminv1.Record {
	v := reflect.ValueOf(itemsAny)
	if !v.IsValid() {
		return nil
	}
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() != reflect.Slice {
		return nil
	}
	out := make([]*adminv1.Record, 0, v.Len())
	for i := 0; i < v.Len(); i++ {
		out = append(out, entityValueToRecord(v.Index(i), meta))
	}
	return out
}

func entityToRecord(entity any, meta *model.ModelMeta) *adminv1.Record {
	v := reflect.ValueOf(entity)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	return entityValueToRecord(v, meta)
}

func entityValueToRecord(v reflect.Value, meta *model.ModelMeta) *adminv1.Record {
	rec := &adminv1.Record{ValuesJson: make(map[string]string, len(meta.Fields))}
	if v.Kind() != reflect.Struct {
		return rec
	}
	for _, f := range meta.Fields {
		if f.IsExcluded {
			continue
		}
		fv := v.FieldByName(f.Name)
		if !fv.IsValid() {
			continue
		}
		raw, err := json.Marshal(normalizeForJSON(fv.Interface()))
		if err != nil {
			continue
		}
		rec.ValuesJson[f.Name] = string(raw)
	}
	return rec
}

func normalizeForJSON(v any) any {
	if t, ok := v.(time.Time); ok {
		if t.IsZero() {
			return nil
		}
		return t.UTC().Format(time.RFC3339)
	}
	return v
}

// buildEntityFromRecord constructs a fresh *T (where T = meta.Type) and
// populates fields from the record's JSON values. Read-only and
// excluded fields are ignored.
func buildEntityFromRecord(rec *adminv1.Record, meta *model.ModelMeta) (any, error) {
	if rec == nil {
		return nil, errors.New("admin agent: record is required")
	}
	ptr := reflect.New(meta.Type)
	v := ptr.Elem()

	if err := applyRecordToValue(rec, meta, v, true); err != nil {
		return nil, err
	}
	return ptr.Interface(), nil
}

// recordToUpdates extracts a column->value map suitable for
// model.CRUD.Update from a record. Read-only / excluded fields are
// silently dropped.
func recordToUpdates(rec *adminv1.Record, meta *model.ModelMeta) (map[string]any, error) {
	if rec == nil {
		return nil, errors.New("admin agent: record is required")
	}
	out := make(map[string]any, len(rec.ValuesJson))
	for _, f := range meta.Fields {
		if f.IsPK || f.IsReadOnly || f.IsExcluded {
			continue
		}
		raw, ok := rec.ValuesJson[f.Name]
		if !ok {
			continue
		}
		val, err := decodeFieldValue(raw, f)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", f.Name, err)
		}
		out[f.Column] = val
	}
	return out, nil
}

func applyRecordToValue(rec *adminv1.Record, meta *model.ModelMeta, v reflect.Value, includePKWhenSet bool) error {
	for _, f := range meta.Fields {
		if f.IsExcluded {
			continue
		}
		raw, ok := rec.ValuesJson[f.Name]
		if !ok {
			continue
		}
		if f.IsPK && !includePKWhenSet {
			continue
		}
		fv := v.FieldByName(f.Name)
		if !fv.IsValid() || !fv.CanSet() {
			continue
		}
		val, err := decodeFieldValue(raw, f)
		if err != nil {
			return fmt.Errorf("field %q: %w", f.Name, err)
		}
		if val == nil {
			fv.Set(reflect.Zero(fv.Type()))
			continue
		}
		converted, err := convertToFieldType(val, fv.Type())
		if err != nil {
			return fmt.Errorf("field %q: %w", f.Name, err)
		}
		fv.Set(converted)
	}
	return nil
}

func decodeFieldValue(raw string, f model.FieldMeta) (any, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" {
		return nil, nil
	}
	switch strings.ToLower(f.GoType) {
	case "time.time":
		var s string
		if err := json.Unmarshal([]byte(raw), &s); err != nil {
			return nil, err
		}
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return nil, err
		}
		return t, nil
	}
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return nil, err
	}
	return v, nil
}

func convertToFieldType(in any, t reflect.Type) (reflect.Value, error) {
	if in == nil {
		return reflect.Zero(t), nil
	}
	v := reflect.ValueOf(in)
	if v.Type().ConvertibleTo(t) {
		return v.Convert(t), nil
	}
	// Numeric upgrades: JSON unmarshals integers into float64; convert.
	switch t.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if f, ok := in.(float64); ok {
			return reflect.ValueOf(int64(f)).Convert(t), nil
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if f, ok := in.(float64); ok {
			return reflect.ValueOf(uint64(f)).Convert(t), nil
		}
	case reflect.Float32, reflect.Float64:
		if f, ok := in.(float64); ok {
			return reflect.ValueOf(f).Convert(t), nil
		}
	case reflect.Bool:
		if b, ok := in.(bool); ok {
			return reflect.ValueOf(b), nil
		}
	}
	return reflect.Value{}, fmt.Errorf("cannot convert %T into %s", in, t)
}

func parseID(raw string, meta *model.ModelMeta) (any, error) {
	id := strings.TrimSpace(raw)
	if id == "" {
		return nil, errors.New("admin agent: id is required")
	}
	pk := meta.PrimaryKey
	if pk == "" {
		pk = "ID"
	}
	for _, f := range meta.Fields {
		if f.Name != pk {
			continue
		}
		switch strings.ToLower(f.GoType) {
		case "int", "int8", "int16", "int32", "int64":
			n, err := strconv.ParseInt(id, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("admin agent: id %q is not an integer", id)
			}
			return n, nil
		case "uint", "uint8", "uint16", "uint32", "uint64":
			n, err := strconv.ParseUint(id, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("admin agent: id %q is not an unsigned integer", id)
			}
			return n, nil
		}
		break
	}
	return id, nil
}
