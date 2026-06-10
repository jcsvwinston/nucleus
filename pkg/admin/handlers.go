package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
	"github.com/jcsvwinston/nucleus/pkg/model"
	"github.com/jcsvwinston/nucleus/pkg/router"
)

// handleListModels returns all registered models with their record counts.
func (p *Panel) handleListModels(c *router.Context) error {
	r := c.Request
	if err := p.authorizeAction(c, "*", "list_models"); err != nil {
		return err
	}
	includeCounts := includeModelCounts(r)

	type modelInfo struct {
		Name        string           `json:"name"`
		Plural      string           `json:"plural"`
		Table       string           `json:"table"`
		Icon        string           `json:"icon"`
		Count       int64            `json:"count"`
		CountKnown  bool             `json:"count_known"`
		IsEstimated bool             `json:"is_estimated"`
		Counts      map[string]int64 `json:"counts,omitempty"`
		Databases   []string         `json:"databases,omitempty"`
		Database    string           `json:"database"`
		Engine      string           `json:"engine"`
	}
	type runtimeModelInfo struct {
		Name        string `json:"name"`
		Plural      string `json:"plural"`
		Table       string `json:"table"`
		Count       int64  `json:"count"`
		CountKnown  bool   `json:"count_known"`
		IsEstimated bool   `json:"is_estimated"`
	}
	type runtimeDatabaseInfo struct {
		Alias        string             `json:"alias"`
		Engine       string             `json:"engine"`
		Dialect      string             `json:"dialect"`
		IsDefault    bool               `json:"is_default"`
		Models       []string           `json:"models"`
		ModelEntries []runtimeModelInfo `json:"model_entries"`
		ModelCount   int                `json:"model_count"`
	}
	type runtimeEngineInfo struct {
		Name      string                `json:"name"`
		Databases []runtimeDatabaseInfo `json:"databases"`
	}
	type runtimeInfo struct {
		Environment      string                `json:"environment"`
		Databases        []runtimeDatabaseInfo `json:"databases"`
		Engines          []string              `json:"engines"`
		EngineGroups     []runtimeEngineInfo   `json:"engine_groups"`
		TraceURLTemplate string                `json:"trace_url_template,omitempty"`
		ModelsTotal      int                   `json:"models_total"`
		RecordsTotal     int64                 `json:"records_total"`
		CountsMode       string                `json:"counts_mode"`
		CountsAvailable  bool                  `json:"counts_available"`
		SessionsCount    int                   `json:"sessions_active"`

		// Multi-tenant/site info
		MultiTenantEnabled bool     `json:"multi_tenant_enabled"`
		MultiTenantDefault string   `json:"multi_tenant_default"`
		TenantIDs          []string `json:"tenant_ids,omitempty"`
		MultiSiteEnabled   bool     `json:"multi_site_enabled"`
		MultiSiteDefault   string   `json:"multi_site_default"`
		SiteNames          []string `json:"site_names,omitempty"`
	}

	models := p.registry.All()
	result := make([]modelInfo, 0, len(models))
	modelByName := make(map[string]*modelInfo, len(models))
	for _, m := range models {
		count := int64(0)
		if !includeCounts {
			count = -1
		}
		info := modelInfo{
			Name:       m.Name,
			Plural:     m.Plural,
			Table:      m.Table,
			Icon:       m.Config.Icon,
			Count:      count,
			CountKnown: false,
			Counts:     map[string]int64{},
			// Filled from probed table PRESENCE per alias below (both count
			// modes), so multi-database topologies (e.g. tenant-isolated
			// schemas) attribute each model to the databases that really
			// hold its table — not just the declared alias. Falls back to
			// the declared/default alias when nothing is probed.
			Databases: []string{},
			Database:  m.DatabaseAlias,
		}
		if info.Database == "" {
			info.Database = "default"
		}
		if dbInfo, ok := p.databaseRuntimeInfoByAlias(info.Database); ok {
			info.Engine = dbInfo.Dialect
			if info.Engine == "" {
				info.Engine = dbInfo.Engine
			}
		}
		result = append(result, info)
		modelByName[m.Name] = &result[len(result)-1]
	}
	sort.SliceStable(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	aliases := p.sortedDatabaseAliases()
	dbRuntime := make([]runtimeDatabaseInfo, 0, len(aliases))
	engineGroups := map[string][]runtimeDatabaseInfo{}
	enginesSeen := map[string]struct{}{}
	modelRecordsByAlias := map[string]map[string]int64{}
	defaultAlias := p.defaultDBAlias

	for _, alias := range aliases {
		cfg, ok := p.databaseRuntimeInfoByAlias(alias)
		if !ok {
			cfg = DatabaseRuntimeInfo{
				Alias:     alias,
				Engine:    "",
				Dialect:   "",
				IsDefault: alias == p.defaultDBAlias,
			}
		}

		modelNames := make([]string, 0, len(models))
		modelEntries := make([]runtimeModelInfo, 0, len(models))
		records := map[string]int64{}
		queryable := true
		if _, err := p.resolveDatabaseAlias(alias); err != nil {
			queryable = false
		}

		if includeCounts {
			if queryable {
				for _, m := range models {
					count, estimated, present, err := p.modelCount(r.Context(), m, alias)
					if err != nil {
						return fmt.Errorf("admin.ListModels count alias=%s model=%s: %w", alias, m.Name, err)
					}
					if !present {
						continue
					}
					records[m.Name] = count
					modelNames = append(modelNames, m.Name)
					modelEntries = append(modelEntries, runtimeModelInfo{
						Name:        m.Name,
						Plural:      m.Plural,
						Table:       m.Table,
						Count:       count,
						CountKnown:  true,
						IsEstimated: estimated,
					})

					if mi, ok := modelByName[m.Name]; ok {
						if alias == defaultAlias || (mi.Count == 0 && !mi.CountKnown) {
							mi.Count = count
							mi.CountKnown = true
							mi.IsEstimated = estimated
						}
						mi.Counts[alias] = count

						// Add database alias if not already present
						found := false
						for _, dbName := range mi.Databases {
							if dbName == alias {
								found = true
								break
							}
						}
						if !found {
							mi.Databases = append(mi.Databases, alias)
						}
					}
				}
			}
		} else {
			if queryable {
				for _, m := range models {
					// Fast mode still probes table PRESENCE (a zero-row
					// scan), so database attribution stays truthful without
					// paying for counts.
					if !p.tableExists(r.Context(), alias, m) {
						continue
					}
					modelNames = append(modelNames, m.Name)
					records[m.Name] = -1
					modelEntries = append(modelEntries, runtimeModelInfo{
						Name:       m.Name,
						Plural:     m.Plural,
						Table:      m.Table,
						Count:      -1,
						CountKnown: false,
					})
					if mi, ok := modelByName[m.Name]; ok {
						found := false
						for _, dbName := range mi.Databases {
							if dbName == alias {
								found = true
								break
							}
						}
						if !found {
							mi.Databases = append(mi.Databases, alias)
						}
					}
				}
			}
		}
		sort.Strings(modelNames)
		sort.SliceStable(modelEntries, func(i, j int) bool {
			return modelEntries[i].Name < modelEntries[j].Name
		})
		modelRecordsByAlias[alias] = records

		dbInfo := runtimeDatabaseInfo{
			Alias:        cfg.Alias,
			Engine:       cfg.Engine,
			Dialect:      cfg.Dialect,
			IsDefault:    cfg.IsDefault,
			Models:       modelNames,
			ModelEntries: modelEntries,
			ModelCount:   len(modelNames),
		}
		dbRuntime = append(dbRuntime, dbInfo)

		engineLabel := strings.TrimSpace(cfg.Dialect)
		if engineLabel == "" {
			engineLabel = strings.TrimSpace(cfg.Engine)
		}
		if engineLabel == "" {
			engineLabel = "unknown"
		}
		enginesSeen[engineLabel] = struct{}{}
		engineGroups[engineLabel] = append(engineGroups[engineLabel], dbInfo)
	}

	var recordsTotal int64
	for _, m := range models {
		row := modelByName[m.Name]
		if row == nil {
			continue
		}
		if includeCounts {
			recordsTotal += row.Count
		}
		// Presence probing found no home (unqueryable handles, missing
		// tables): fall back to the declared/default alias so the model is
		// never attributed to zero databases.
		if len(row.Databases) == 0 {
			row.Databases = []string{row.Database}
		}
		sort.Strings(row.Databases)
	}

	engines := make([]string, 0, len(enginesSeen))
	for label := range enginesSeen {
		engines = append(engines, label)
	}
	sort.Strings(engines)

	engineRuntime := make([]runtimeEngineInfo, 0, len(engines))
	for _, engine := range engines {
		rows := engineGroups[engine]
		sort.SliceStable(rows, func(i, j int) bool {
			if rows[i].IsDefault != rows[j].IsDefault {
				return rows[i].IsDefault
			}
			return rows[i].Alias < rows[j].Alias
		})
		engineRuntime = append(engineRuntime, runtimeEngineInfo{
			Name:      engine,
			Databases: rows,
		})
	}

	sessionsCount := 0
	if p.config.Session != nil {
		if payloads, supported, err := allSessionPayloads(r.Context(), p.config.Session); err == nil && supported {
			sessionsCount = len(payloads)
		}
	}

	countsMode := "full"
	if !includeCounts {
		countsMode = "light"
		recordsTotal = -1
	}

	totalModelsAcrossDBs := 0
	for _, dbInfo := range dbRuntime {
		totalModelsAcrossDBs += dbInfo.ModelCount
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"models": result,
		"title":  p.config.Title,
		"runtime": runtimeInfo{
			Environment:        strings.TrimSpace(p.config.Environment),
			Databases:          dbRuntime,
			Engines:            engines,
			EngineGroups:       engineRuntime,
			TraceURLTemplate:   strings.TrimSpace(p.config.TraceURLTemplate),
			ModelsTotal:        totalModelsAcrossDBs,
			RecordsTotal:       recordsTotal,
			CountsMode:         countsMode,
			CountsAvailable:    includeCounts,
			SessionsCount:      sessionsCount,
			MultiTenantEnabled: p.config.MultiTenantEnabled,
			MultiTenantDefault: p.config.MultiTenantDefault,
			TenantIDs:          p.config.MultiTenantIDs,
			MultiSiteEnabled:   p.config.MultiSiteEnabled,
			MultiSiteDefault:   p.config.MultiSiteDefault,
			SiteNames:          p.config.MultiSiteNames,
		},
	})
}

// handleGetSchema returns metadata for a specific model.
func (p *Panel) handleGetSchema(c *router.Context) error {
	name := c.Param("name")
	meta, ok := p.registry.Get(name)
	if !ok {
		return gferrors.NotFound("model", name)
	}
	if err := p.authorizeAction(c, meta.Name, "get_schema"); err != nil {
		return err
	}

	type fieldInfo struct {
		Name          string         `json:"name"`
		Column        string         `json:"column"`
		Label         string         `json:"label"`
		Type          string         `json:"type"`
		HTMLType      string         `json:"html_type"`
		IsPK          bool           `json:"is_pk"`
		IsRequired    bool           `json:"is_required"`
		IsReadOnly    bool           `json:"is_readonly"`
		IsList        bool           `json:"is_list"`
		IsSearch      bool           `json:"is_search"`
		IsFilter      bool           `json:"is_filter"`
		IsExcluded    bool           `json:"is_excluded"`
		IsForeignKey  bool           `json:"is_fk"`
		IsTenantField bool           `json:"is_tenant_field"`
		ForeignModel  string         `json:"fk_model,omitempty"`
		Choices       []model.Choice `json:"choices,omitempty"`
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
			IsTenantField: f.IsTenantField,
			ForeignModel:  f.ForeignModel, Choices: f.Choices,
		})
	}

	tenantField := p.resolveTenantField(meta.Name)
	return c.JSON(http.StatusOK, map[string]interface{}{
		"name":         meta.Name,
		"plural":       meta.Plural,
		"table":        meta.Table,
		"primary_key":  meta.PrimaryKey,
		"icon":         meta.Config.Icon,
		"read_only":    meta.Config.ReadOnly,
		"fields":       fields,
		"foreign_keys": meta.ForeignKeys,
		"tenant_field": tenantField,
	})
}

// handleUpdateFieldMeta updates field metadata properties at runtime (like Django ModelAdmin).
func (p *Panel) handleUpdateFieldMeta(c *router.Context) error {
	r := c.Request
	name := c.Param("name")
	meta, ok := p.registry.Get(name)
	if !ok {
		return gferrors.NotFound("model", name)
	}
	if err := p.authorizeAction(c, meta.Name, "update_schema"); err != nil {
		return err
	}

	var payload struct {
		Fields map[string]model.FieldMetaUpdate `json:"fields"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		return gferrors.BadRequest("invalid JSON: " + err.Error())
	}

	if len(payload.Fields) == 0 {
		return gferrors.BadRequest("no field updates provided")
	}

	if err := p.registry.BulkUpdateFieldMeta(name, payload.Fields); err != nil {
		return gferrors.BadRequest(err.Error())
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"ok":      true,
		"message": fmt.Sprintf("Updated %d field(s) for %s", len(payload.Fields), name),
	})
}

// handleListRecords returns a paginated list of records for a model.
func (p *Panel) handleListRecords(c *router.Context) error {
	r := c.Request
	name := c.Param("name")
	meta, ok := p.registry.Get(name)
	if !ok {
		return gferrors.NotFound("model", name)
	}
	if err := p.authorizeAction(c, meta.Name, "list"); err != nil {
		return err
	}

	databaseAlias, err := p.requestDatabaseAlias(r)
	if err != nil {
		return gferrors.BadRequest(err.Error())
	}
	// Fallback to model's declared database if no explicit override provided in query
	if r.URL.Query().Get("db") == "" && r.URL.Query().Get("database") == "" && r.URL.Query().Get("db_alias") == "" {
		if meta.DatabaseAlias != "" {
			databaseAlias = meta.DatabaseAlias
		}
	}

	crud, err := p.getCRUD(meta, databaseAlias)
	if err != nil {
		return err
	}
	page, pageSet, err := parsePositiveQueryInt(r.URL.Query(), "page")
	if err != nil {
		return err
	}
	pageSize, pageSizeSet, err := parsePositiveQueryInt(r.URL.Query(), "page_size")
	if err != nil {
		return err
	}
	if pageSizeSet && pageSize > 200 {
		return gferrors.BadRequest("page_size must be <= 200")
	}

	search, err := sanitizeSearchQuery(r.URL.Query().Get("search"))
	if err != nil {
		return err
	}

	orderBy, err := sanitizeOrderBy(meta, r.URL.Query().Get("order_by"))
	if err != nil {
		return err
	}

	filters, err := collectFilters(meta, r.URL.Query())
	if err != nil {
		return err
	}

	// Apply tenant filtering when multi-tenant is enabled
	if tenantCtx := tenantContextFromRequest(r); tenantCtx != nil && tenantCtx.Enabled && tenantCtx.AutoFilter {
		tenantField := p.resolveTenantField(meta.Name)
		if tenantField != "" && tenantCtx.TenantID != "" {
			if filters == nil {
				filters = make(map[string]string)
			}
			filters[tenantField] = tenantCtx.TenantID
		}
	}

	if !pageSet {
		page = 0
	}
	if !pageSizeSet {
		pageSize = 0
	}

	result, err := crud.FindAll(r.Context(), model.QueryOpts{
		Page: page, PageSize: pageSize, Search: search,
		Filters: filters, OrderBy: orderBy,
	})
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, result)
}

// handleGetRecord returns a single record by ID.
func (p *Panel) handleGetRecord(c *router.Context) error {
	r := c.Request
	name := c.Param("name")
	idStr := c.Param("id")

	meta, ok := p.registry.Get(name)
	if !ok {
		return gferrors.NotFound("model", name)
	}
	if err := p.authorizeAction(c, meta.Name, "retrieve"); err != nil {
		return err
	}

	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		return gferrors.BadRequest("invalid id")
	}

	databaseAlias, err := p.requestDatabaseAlias(r)
	if err != nil {
		return gferrors.BadRequest(err.Error())
	}
	// Fallback to model's declared database if no explicit override provided in query
	if r.URL.Query().Get("db") == "" && r.URL.Query().Get("database") == "" && r.URL.Query().Get("db_alias") == "" {
		if meta.DatabaseAlias != "" {
			databaseAlias = meta.DatabaseAlias
		}
	}

	crud, err := p.getCRUD(meta, databaseAlias)
	if err != nil {
		return err
	}
	record, err := crud.FindByID(r.Context(), uint(id))
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, record)
}

// handleCreateRecord creates a new record.
func (p *Panel) handleCreateRecord(c *router.Context) error {
	r := c.Request
	name := c.Param("name")
	meta, ok := p.registry.Get(name)
	if !ok {
		return gferrors.NotFound("model", name)
	}
	if err := p.authorizeAction(c, meta.Name, "create"); err != nil {
		return err
	}
	if meta.Config.ReadOnly {
		return gferrors.Forbidden("model is read-only")
	}

	databaseAlias, err := p.requestDatabaseAlias(r)
	if err != nil {
		return gferrors.BadRequest(err.Error())
	}
	// Fallback to model's declared database if no explicit override provided in query
	if r.URL.Query().Get("db") == "" && r.URL.Query().Get("database") == "" && r.URL.Query().Get("db_alias") == "" {
		if meta.DatabaseAlias != "" {
			databaseAlias = meta.DatabaseAlias
		}
	}

	crud, err := p.getCRUD(meta, databaseAlias)
	if err != nil {
		return err
	}

	var data map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		return gferrors.BadRequest("invalid JSON: " + err.Error())
	}

	// Auto-inject tenant ID on create when multi-tenant is enabled
	if tenantCtx := tenantContextFromRequest(r); tenantCtx != nil && tenantCtx.Enabled && tenantCtx.TenantID != "" {
		tenantField := p.resolveTenantField(meta.Name)
		if tenantField != "" {
			// Only inject if not already provided in payload
			if _, exists := data[tenantField]; !exists {
				// Also check Go field name variant
				goFieldName := columnToGoField(meta, tenantField)
				if goFieldName != "" {
					if _, exists2 := data[goFieldName]; !exists2 {
						data[tenantField] = tenantCtx.TenantID
					}
				} else {
					data[tenantField] = tenantCtx.TenantID
				}
			}
		}
	}

	entity, err := payloadToEntity(meta, data)
	if err != nil {
		return err
	}

	if err := crud.Create(r.Context(), entity); err != nil {
		return err
	}

	return c.JSON(http.StatusCreated, entity)
}

// handleUpdateRecord updates an existing record.
func (p *Panel) handleUpdateRecord(c *router.Context) error {
	r := c.Request
	name := c.Param("name")
	idStr := c.Param("id")

	meta, ok := p.registry.Get(name)
	if !ok {
		return gferrors.NotFound("model", name)
	}
	if err := p.authorizeAction(c, meta.Name, "update"); err != nil {
		return err
	}
	if meta.Config.ReadOnly {
		return gferrors.Forbidden("model is read-only")
	}

	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		return gferrors.BadRequest("invalid id")
	}

	var updates map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		return gferrors.BadRequest("invalid JSON")
	}

	databaseAlias, err := p.requestDatabaseAlias(r)
	if err != nil {
		return gferrors.BadRequest(err.Error())
	}
	// Fallback to model's declared database if no explicit override provided in query
	if r.URL.Query().Get("db") == "" && r.URL.Query().Get("database") == "" && r.URL.Query().Get("db_alias") == "" {
		if meta.DatabaseAlias != "" {
			databaseAlias = meta.DatabaseAlias
		}
	}

	crud, err := p.getCRUD(meta, databaseAlias)
	if err != nil {
		return err
	}
	if err := crud.Update(r.Context(), uint(id), updates); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, map[string]interface{}{"updated": true, "id": id})
}

// handleDeleteRecord deletes a record by ID.
func (p *Panel) handleDeleteRecord(c *router.Context) error {
	r := c.Request
	name := c.Param("name")
	idStr := c.Param("id")

	meta, ok := p.registry.Get(name)
	if !ok {
		return gferrors.NotFound("model", name)
	}
	if err := p.authorizeAction(c, meta.Name, "delete"); err != nil {
		return err
	}
	if meta.Config.ReadOnly {
		return gferrors.Forbidden("model is read-only")
	}

	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		return gferrors.BadRequest("invalid id")
	}

	databaseAlias, err := p.requestDatabaseAlias(r)
	if err != nil {
		return gferrors.BadRequest(err.Error())
	}
	// Fallback to model's declared database if no explicit override provided in query
	if r.URL.Query().Get("db") == "" && r.URL.Query().Get("database") == "" && r.URL.Query().Get("db_alias") == "" {
		if meta.DatabaseAlias != "" {
			databaseAlias = meta.DatabaseAlias
		}
	}

	crud, err := p.getCRUD(meta, databaseAlias)
	if err != nil {
		return err
	}
	if err := crud.Delete(r.Context(), uint(id)); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, map[string]interface{}{"deleted": true, "id": id})
}

// handleBulkAction processes bulk operations (delete, export).
func (p *Panel) handleBulkAction(c *router.Context) error {
	r := c.Request
	name := c.Param("name")
	meta, ok := p.registry.Get(name)
	if !ok {
		return gferrors.NotFound("model", name)
	}

	var req struct {
		Action string `json:"action"`
		IDs    []uint `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return gferrors.BadRequest("invalid JSON")
	}

	databaseAlias, err := p.requestDatabaseAlias(r)
	if err != nil {
		return gferrors.BadRequest(err.Error())
	}
	// Fallback to model's declared database if no explicit override provided in query
	if r.URL.Query().Get("db") == "" && r.URL.Query().Get("database") == "" && r.URL.Query().Get("db_alias") == "" {
		if meta.DatabaseAlias != "" {
			databaseAlias = meta.DatabaseAlias
		}
	}

	action := strings.ToLower(strings.TrimSpace(req.Action))
	switch action {
	case "delete":
		if err := p.authorizeAction(c, meta.Name, "bulk_delete"); err != nil {
			return err
		}
		if meta.Config.ReadOnly {
			return gferrors.Forbidden("model is read-only")
		}
		if len(req.IDs) == 0 {
			return gferrors.BadRequest("ids are required for delete action")
		}
		crud, err := p.getCRUD(meta, databaseAlias)
		if err != nil {
			return err
		}

		type bulkDeleteError struct {
			ID    uint   `json:"id"`
			Error string `json:"error"`
		}

		deleted := 0
		failures := make([]bulkDeleteError, 0)
		for _, id := range req.IDs {
			deleteErr := crud.Delete(r.Context(), id)
			if deleteErr == nil {
				deleted++
				continue
			}
			failures = append(failures, bulkDeleteError{
				ID:    id,
				Error: deleteErr.Error(),
			})
		}
		return c.JSON(http.StatusOK, map[string]interface{}{
			"action":    "delete",
			"requested": len(req.IDs),
			"deleted":   deleted,
			"failed":    len(failures),
			"errors":    failures,
		})

	case "export":
		if err := p.authorizeAction(c, meta.Name, "bulk_export"); err != nil {
			return err
		}
		if len(req.IDs) == 0 {
			return gferrors.BadRequest("ids are required for export action")
		}
		return c.JSON(http.StatusOK, map[string]interface{}{
			"export_url": buildBulkExportURL(r.URL.Path, req.IDs, databaseAlias),
			"ids":        req.IDs,
		})

	default:
		return gferrors.BadRequest("unknown action: " + req.Action)
	}
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeErr(w http.ResponseWriter, r *http.Request, err error) {
	gferrors.WriteError(w, r, err, nil)
}

// authErrorToDomain converts an AdminAuth.Authenticate failure into the
// client-facing 401. The client always sees a fixed "authentication
// required" message: the raw provider error can carry internal detail
// (DB connectivity, internal state, secrets embedded in a DSN) and must
// never leak to an unauthenticated caller. The raw error is logged
// server-side at Debug for diagnostics. Hardening from the ADR-016 review.
func (p *Panel) authErrorToDomain(err error) error {
	if err != nil {
		// Log via the panel logger when available, else the default —
		// never silently drop the diagnostic (matches warnAdminAuthDisabled).
		lg := slog.Default()
		if p != nil && p.logger != nil {
			lg = p.logger
		}
		lg.Debug("admin authentication failed", "error", err.Error())
	}
	return gferrors.Unauthorized("authentication required")
}

func authDeniedDomain(modelName, action string) error {
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		modelName = "*"
	}
	action = strings.TrimSpace(action)
	if action == "" {
		action = "unknown"
	}
	return gferrors.Forbidden(fmt.Sprintf("not authorized to %s on %s", action, modelName))
}

func includeModelCounts(r *http.Request) bool {
	if r == nil {
		return true
	}
	stats := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("stats")))
	switch stats {
	case "light", "lite", "meta", "fast", "no-counts", "nocounts":
		return false
	case "full", "counts":
		return true
	}

	counts := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("counts")))
	switch counts {
	case "0", "false", "off", "no":
		return false
	case "1", "true", "on", "yes":
		return true
	}
	return true
}

func (p *Panel) modelCount(ctx context.Context, meta *model.ModelMeta, databaseAlias string) (count int64, isEstimated bool, present bool, err error) {
	handle, err := p.databaseHandle(databaseAlias)
	if err != nil {
		return 0, false, false, err
	}
	sqlDB, err := handle.SqlDB()
	if err != nil {
		return 0, false, false, err
	}

	table := meta.Table
	if table == "" {
		table = strings.ToLower(meta.Name) + "s"
	}

	info, _ := p.databaseRuntimeInfoByAlias(databaseAlias)
	dialect := strings.ToLower(info.Dialect)
	if dialect == "" {
		dialect = strings.ToLower(info.Engine)
	}

	var total int64
	var query string
	estimated := false

	switch dialect {
	case "postgres":
		query = "SELECT reltuples::bigint FROM pg_class WHERE relname = ?"
		estimated = true
	case "mysql":
		query = "SELECT TABLE_ROWS FROM information_schema.tables WHERE table_name = ? AND table_schema = DATABASE()"
		estimated = true
	case "sqlite", "sqlite3":
		query = "SELECT n FROM sqlite_stat1 WHERE tbl = ? LIMIT 1"
		estimated = true
	case "sqlserver", "mssql":
		query = "SELECT SUM(rows) FROM sys.partitions WHERE object_id = OBJECT_ID(?) AND index_id IN (0, 1)"
		estimated = true
	case "oracle":
		query = "SELECT NUM_ROWS FROM ALL_TABLES WHERE TABLE_NAME = UPPER(?)"
		estimated = true
	default:
		query = fmt.Sprintf("SELECT COUNT(*) FROM %s", table)
	}

	var errQuery error
	if estimated {
		errQuery = sqlDB.QueryRowContext(ctx, query, table).Scan(&total)
		if errQuery != nil {
			// Fallback to real count if estimate fails or table not analyzed
			query = fmt.Sprintf("SELECT COUNT(*) FROM %s", table)
			errQuery = sqlDB.QueryRowContext(ctx, query).Scan(&total)
			estimated = false
		}
	} else {
		errQuery = sqlDB.QueryRowContext(ctx, query).Scan(&total)
	}

	if errQuery != nil {
		if isTableMissingErr(errQuery) {
			return 0, false, false, nil
		}
		return 0, false, false, fmt.Errorf("admin.modelCount table=%s: %w", table, errQuery)
	}
	return total, estimated, true, nil
}

// tableExists probes whether the model's table is present on the given
// database alias with a zero-row scan (`WHERE 1=0`) — cheap on every engine,
// no counting. The table name comes from registered model metadata (already
// identifier-gated at registration), never from request input.
func (p *Panel) tableExists(ctx context.Context, databaseAlias string, meta *model.ModelMeta) bool {
	handle, err := p.databaseHandle(databaseAlias)
	if err != nil {
		return false
	}
	sqlDB, err := handle.SqlDB()
	if err != nil {
		return false
	}
	table := meta.Table
	if table == "" {
		table = strings.ToLower(meta.Name) + "s"
	}
	rows, err := sqlDB.QueryContext(ctx, fmt.Sprintf("SELECT 1 FROM %s WHERE 1=0", table))
	if err != nil {
		return false
	}
	_ = rows.Close()
	return rows.Err() == nil
}

func isTableMissingErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	if msg == "" {
		return false
	}
	return strings.Contains(msg, "no such table") ||
		strings.Contains(msg, "does not exist") ||
		strings.Contains(msg, "unknown table") ||
		strings.Contains(msg, "undefined table")
}

func (p *Panel) databaseRuntimeInfoByAlias(alias string) (DatabaseRuntimeInfo, bool) {
	needle := strings.TrimSpace(alias)
	if needle == "" {
		return DatabaseRuntimeInfo{}, false
	}
	for _, item := range p.config.Databases {
		if strings.TrimSpace(item.Alias) == needle {
			return item, true
		}
	}
	return DatabaseRuntimeInfo{}, false
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

// sanitizeOrderBy validates the admin list endpoint's order_by parameter.
// It delegates to model.SanitizeOrderBy — the single order-by allow-list
// shared with the CRUD layer (audit LOW-B), so the admin cannot drift from
// (or be laxer than) the layer that ultimately runs the query — and maps
// any rejection to a 400. As a result the admin now also accepts
// comma-separated multi-column ordering, exactly as the CRUD layer does.
func sanitizeOrderBy(meta *model.ModelMeta, raw string) (string, error) {
	orderBy, err := model.SanitizeOrderBy(meta, raw)
	if err != nil {
		return "", gferrors.BadRequest("invalid order_by")
	}
	return orderBy, nil
}

func parsePositiveQueryInt(values url.Values, key string) (value int, provided bool, err error) {
	raw := strings.TrimSpace(values.Get(key))
	if raw == "" {
		return 0, false, nil
	}

	n, convErr := strconv.Atoi(raw)
	if convErr != nil {
		return 1, true, nil // Be lenient with invalid inputs
	}
	if n <= 0 {
		return 1, true, nil // Normalize to 1 for pagination
	}
	return n, true, nil
}

func sanitizeSearchQuery(raw string) (string, error) {
	search := strings.TrimSpace(raw)
	if len(search) > 256 {
		return "", gferrors.BadRequest("search is too long (max 256 characters)")
	}
	return search, nil
}

func collectFilters(meta *model.ModelMeta, values url.Values) (map[string]string, error) {
	filters := make(map[string]string)
	for key, vals := range values {
		// Reserved query parameters consumed by the handler itself. db_alias
		// is the database-selector spelling the admin UI sends (the Data
		// Studio database pills); omitting it here rejected every
		// non-default-database listing with `invalid filter field "db_alias"`.
		if key == "page" || key == "page_size" || key == "search" || key == "order_by" ||
			key == "db" || key == "database" || key == "db_alias" {
			continue
		}
		if len(vals) == 0 {
			continue
		}

		raw := strings.TrimSpace(vals[0])
		if raw == "" {
			continue
		}

		col, normalized, err := normalizeFilter(meta, key, raw)
		if err != nil {
			return nil, err
		}
		filters[col] = normalized
	}
	return filters, nil
}

func normalizeFilter(meta *model.ModelMeta, key, value string) (column, normalized string, err error) {
	col, field, found := resolveField(meta, key)
	if !found {
		return "", "", gferrors.BadRequest(fmt.Sprintf("invalid filter field %q", key))
	}
	if !field.IsFilter {
		return "", "", gferrors.BadRequest(fmt.Sprintf("filter is not enabled for %q", key))
	}

	normalized = value
	if strings.EqualFold(field.GoType, "bool") {
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "1", "true", "yes", "on":
			normalized = "1"
		case "0", "false", "no", "off":
			normalized = "0"
		default:
			return "", "", gferrors.BadRequest(fmt.Sprintf("invalid boolean value %q for filter %q", value, key))
		}
	}

	return col, normalized, nil
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

func buildBulkExportURL(currentPath string, ids []uint, databaseAlias string) string {
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
	if alias := strings.TrimSpace(databaseAlias); alias != "" {
		q.Set("db", alias)
	}
	return base + "/export?" + q.Encode()
}

// columnToGoField converts a database column name to a Go struct field name.
func columnToGoField(meta *model.ModelMeta, column string) string {
	for _, f := range meta.Fields {
		if f.Column == column {
			return f.Name
		}
	}
	return ""
}
