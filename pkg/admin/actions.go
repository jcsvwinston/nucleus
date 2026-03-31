package admin

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	gferrors "github.com/jcsvwinston/GoFrame/pkg/errors"
	"github.com/jcsvwinston/GoFrame/pkg/model"
)

// handleExportCSV exports all records of a model as CSV.
func (p *Panel) handleExportCSV(w http.ResponseWriter, r *http.Request) {
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
	idSet, err := parseIDSet(r.URL.Query().Get("ids"))
	if err != nil {
		writeErr(w, gferrors.BadRequest("invalid ids query param"))
		return
	}
	result, err := crud.FindAll(r.Context(), model.QueryOpts{
		Page: 1, PageSize: 10000,
	})
	if err != nil {
		writeErr(w, err)
		return
	}

	// Determine visible columns
	var headers []string
	var columns []string
	for _, f := range meta.Fields {
		if !f.IsExcluded {
			headers = append(headers, f.Label)
			columns = append(columns, f.Name)
		}
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.csv"`, meta.Table))

	writer := csv.NewWriter(w)
	defer writer.Flush()

	writer.Write(headers)

	// Iterate over items using reflection
	items := reflect.ValueOf(result.Items)
	for i := 0; i < items.Len(); i++ {
		item := items.Index(i)
		if item.Kind() == reflect.Ptr {
			item = item.Elem()
		}
		if len(idSet) > 0 {
			id, ok := recordID(item)
			if !ok {
				continue
			}
			if _, exists := idSet[id]; !exists {
				continue
			}
		}
		row := make([]string, 0, len(columns))
		for _, col := range columns {
			field := item.FieldByName(col)
			if field.IsValid() {
				row = append(row, fmt.Sprintf("%v", field.Interface()))
			} else {
				row = append(row, "")
			}
		}
		writer.Write(row)
	}
}

func parseIDSet(raw string) (map[uint64]struct{}, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[uint64]struct{}{}, nil
	}

	set := make(map[uint64]struct{})
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			return nil, err
		}
		set[id] = struct{}{}
	}
	return set, nil
}

func recordID(item reflect.Value) (uint64, bool) {
	idField := item.FieldByName("ID")
	if !idField.IsValid() {
		idField = item.FieldByName("Id")
	}
	if !idField.IsValid() {
		return 0, false
	}

	switch idField.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return idField.Uint(), true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if idField.Int() < 0 {
			return 0, false
		}
		return uint64(idField.Int()), true
	default:
		return 0, false
	}
}
