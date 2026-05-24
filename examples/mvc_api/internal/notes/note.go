// Package notes is the mvc_api example module for managing short notes.
// It demonstrates a Nucleus Module[C] with a REST Resource controller.
package notes

import (
	"time"

	"github.com/jcsvwinston/nucleus/pkg/model"
)

// Note is the domain model for a short text note.
// It embeds model.BaseModel for the standard id/created_at/updated_at/deleted_at fields.
type Note struct {
	model.BaseModel

	Title string `db:"required"          json:"title"      validate:"required"`
	Body  string `db:"column:body"       json:"body"`
}

// noteRow mirrors the SQL columns returned by a SELECT and is used for
// lightweight scanning without reflection — appropriate for a teaching example.
type noteRow struct {
	ID        uint       `json:"id"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `json:"-"`
}
