package model

import (
	"fmt"
	"sort"
	"sync"
)

// ModelConfig holds user-provided configuration for a registered model.
type ModelConfig struct {
	Icon          string            // Emoji or icon identifier for the admin sidebar
	ListFields    []string          // Fields shown in the list view
	SearchFields  []string          // Fields included in search queries
	Filters       []string          // Fields shown as filters
	OrderBy       string            // Default ordering (e.g. "created_at desc")
	PageSize      int               // Default page size (0 = framework default of 25)
	ReadOnly      bool              // If true, no create/update/delete in admin
	ExcludeFields []string          // Fields excluded from admin
	FieldLabels   map[string]string // Custom labels: field name -> label

	// RejectClientPK makes CRUD.Create return ErrClientAssignedPK when the
	// entity arrives with a non-zero primary key, instead of inserting that
	// key. Off by default: a caller-assigned key travels in the INSERT
	// (client-generated UUIDs, natural keys).
	//
	// Turn it on for models whose HTTP handlers decode a request body
	// straight into the entity (BindJSON + Create): with the default, that
	// pattern lets the HTTP client choose the row's key. The check runs at
	// the top of Create, before hooks — a BeforeCreate hook that assigns a
	// server-generated key still works with this enabled.
	RejectClientPK bool

	// Database affinity
	DatabaseAlias string // Optional database alias for this model (default "default")

	// Lifecycle hooks
	BeforeCreate HookFunc
	AfterCreate  HookFunc
	BeforeUpdate HookFunc
	AfterUpdate  HookFunc
	BeforeDelete HookFunc
}

// Registry stores registered models and their metadata.
// It is the central model catalog, equivalent to Django's AppRegistry.
type Registry struct {
	mu     sync.RWMutex
	models map[string]*ModelMeta
}

// NewRegistry creates an empty model registry.
func NewRegistry() *Registry {
	return &Registry{
		models: make(map[string]*ModelMeta),
	}
}

// Register extracts metadata from a model struct and stores it in the registry.
// Optional ModelConfig overrides can customize admin behavior.
func (r *Registry) Register(model interface{}, cfg ...ModelConfig) error {
	meta, err := ExtractMeta(model)
	if err != nil {
		return fmt.Errorf("model.Registry.Register: %w", err)
	}

	// Apply user config
	if len(cfg) > 0 {
		c := cfg[0]
		meta.Config = c

		// Apply config overrides to field metadata
		r.applyConfig(meta, c)
	}

	// Apply defaults
	if meta.Config.PageSize == 0 {
		meta.Config.PageSize = 25
	}
	meta.DatabaseAlias = meta.Config.DatabaseAlias
	if meta.DatabaseAlias == "" {
		meta.DatabaseAlias = "default"
	}

	r.mu.Lock()
	r.models[meta.Name] = meta
	r.mu.Unlock()

	return nil
}

// Get returns the metadata for a model by name (case-sensitive).
func (r *Registry) Get(name string) (*ModelMeta, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	m, ok := r.models[name]
	return m, ok
}

// All returns all registered models sorted alphabetically by name.
func (r *Registry) All() []*ModelMeta {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*ModelMeta, 0, len(r.models))
	for _, m := range r.models {
		result = append(result, m)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

// Count returns the number of registered models.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.models)
}

// TenantFieldName returns the name of the tenant field column for a model, if declared.
func (m *ModelMeta) TenantFieldName() string {
	for _, f := range m.Fields {
		if f.IsTenantField {
			return f.Column
		}
	}
	// Convention: check common tenant field names as fallback
	for _, candidate := range []string{"tenant_id", "tenant"} {
		for _, f := range m.Fields {
			if f.Column == candidate {
				return f.Column
			}
		}
	}
	return ""
}

// applyConfig applies ModelConfig overrides to the extracted field metadata.
func (r *Registry) applyConfig(meta *ModelMeta, cfg ModelConfig) {
	// Build lookup sets for O(1) membership tests.
	listSet := toSet(cfg.ListFields)
	searchSet := toSet(cfg.SearchFields)
	filterSet := toSet(cfg.Filters)
	excludeSet := toSet(cfg.ExcludeFields)

	for i := range meta.Fields {
		f := &meta.Fields[i]

		// ListFields override: if specified, only those fields are listed.
		if len(listSet) > 0 {
			f.IsList = listSet[f.Name]
		}
		if searchSet[f.Name] {
			f.IsSearch = true
		}
		if filterSet[f.Name] {
			f.IsFilter = true
		}
		if excludeSet[f.Name] {
			f.IsExcluded = true
		}
		if label, ok := cfg.FieldLabels[f.Name]; ok {
			f.Label = label
		}
	}
}

// FieldMetaUpdate holds the mutable field properties that can be changed at runtime
// via the admin panel (like Django's ModelAdmin configuration).
type FieldMetaUpdate struct {
	IsList     *bool   `json:"is_list,omitempty"`
	IsSearch   *bool   `json:"is_search,omitempty"`
	IsFilter   *bool   `json:"is_filter,omitempty"`
	IsExcluded *bool   `json:"is_excluded,omitempty"`
	IsReadOnly *bool   `json:"is_readonly,omitempty"`
	Label      *string `json:"label,omitempty"`
	HTMLType   *string `json:"html_type,omitempty"`
}

// UpdateFieldMeta updates mutable properties of a field at runtime.
// Returns an error if the model or field is not found.
func (r *Registry) UpdateFieldMeta(modelName, fieldName string, update FieldMetaUpdate) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	meta, ok := r.models[modelName]
	if !ok {
		return fmt.Errorf("model %q not found", modelName)
	}

	for i := range meta.Fields {
		if meta.Fields[i].Name == fieldName || meta.Fields[i].Column == fieldName {
			f := &meta.Fields[i]
			if update.IsList != nil {
				f.IsList = *update.IsList
			}
			if update.IsSearch != nil {
				f.IsSearch = *update.IsSearch
			}
			if update.IsFilter != nil {
				f.IsFilter = *update.IsFilter
			}
			if update.IsExcluded != nil {
				f.IsExcluded = *update.IsExcluded
			}
			if update.IsReadOnly != nil {
				f.IsReadOnly = *update.IsReadOnly
			}
			if update.Label != nil && *update.Label != "" {
				f.Label = *update.Label
			}
			if update.HTMLType != nil && *update.HTMLType != "" {
				f.HTMLType = *update.HTMLType
			}
			return nil
		}
	}

	return fmt.Errorf("field %q not found in model %q", fieldName, modelName)
}

// BulkUpdateFieldMeta updates multiple fields at once for a model.
func (r *Registry) BulkUpdateFieldMeta(modelName string, updates map[string]FieldMetaUpdate) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	meta, ok := r.models[modelName]
	if !ok {
		return fmt.Errorf("model %q not found", modelName)
	}

	for i := range meta.Fields {
		f := &meta.Fields[i]
		upd, found := updates[f.Name]
		if !found {
			upd, found = updates[f.Column]
		}
		if !found {
			continue
		}
		if upd.IsList != nil {
			f.IsList = *upd.IsList
		}
		if upd.IsSearch != nil {
			f.IsSearch = *upd.IsSearch
		}
		if upd.IsFilter != nil {
			f.IsFilter = *upd.IsFilter
		}
		if upd.IsExcluded != nil {
			f.IsExcluded = *upd.IsExcluded
		}
		if upd.IsReadOnly != nil {
			f.IsReadOnly = *upd.IsReadOnly
		}
		if upd.Label != nil && *upd.Label != "" {
			f.Label = *upd.Label
		}
		if upd.HTMLType != nil && *upd.HTMLType != "" {
			f.HTMLType = *upd.HTMLType
		}
	}

	return nil
}

func toSet(slice []string) map[string]bool {
	if len(slice) == 0 {
		return nil
	}
	m := make(map[string]bool, len(slice))
	for _, s := range slice {
		m[s] = true
	}
	return m
}
