// Package dupfixture holds a model whose type NAME collides with one in the
// model package's tests: two packages each declaring a Product is the
// ordinary way two modules end up registering the same model name (NU-20).
package dupfixture

// Product collides by name with the test's Product.
type Product struct {
	ID    int     `db:"id" json:"id"`
	Price float64 `db:"price" json:"price"`
}
