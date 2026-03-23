package admin

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"reflect"

	"github.com/go-chi/chi/v5"
	gferrors "github.com/goframe/goframe/pkg/errors"
	"github.com/goframe/goframe/pkg/model"
)

// handleExportCSV exports all records of a model as CSV.
func (p *Panel) handleExportCSV(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	meta, ok := p.registry.Get(name)
	if !ok {
		writeErr(w, gferrors.NotFound("model", name))
		return
	}

	crud := p.getCRUD(meta)
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
