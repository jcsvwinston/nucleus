// Package shop is the showcase's domain: two Quark models and the HTTP routes
// that exercise them, so Orbit's live SQL feed has real traffic to show.
package shop

// Author is a Quark model (Active Record struct with Quark tags).
type Author struct {
	ID   int64  `db:"id" pk:"true"`
	Name string `db:"name" quark:"not_null"`
}

// Article belongs to an Author. The rel tag lets Data Studio surface the
// relationship (quarkdatasource maps belongs_to to a foreign key).
type Article struct {
	ID       int64  `db:"id" pk:"true"`
	AuthorID int64  `db:"author_id" quark:"not_null"`
	Title    string `db:"title" quark:"not_null"`
	Body     string `db:"body"`

	Author Author `rel:"belongs_to" join:"author_id"`
}
