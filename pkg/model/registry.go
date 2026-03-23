package model

import (
	"fmt"
	"sort"
	"sync"

	"gorm.io/gorm"
)

// HookFunc is the signature for model lifecycle hooks.
type HookFunc func(db *gorm.DB, entity interface{}) error

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
