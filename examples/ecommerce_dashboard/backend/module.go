package main

import (
	"github.com/jcsvwinston/nucleus/examples/ecommerce_dashboard/backend/handlers"
	"github.com/jcsvwinston/nucleus/examples/ecommerce_dashboard/backend/models"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
)

// EcommerceConfig is the module-specific configuration shape. Today it
// has no settable fields; the type exists so callers can extend it
// without rewriting the Module instantiation. Phase 2 (config loading)
// will bind `modules.ecommerce.*` from the YAML config into this
// struct.
type EcommerceConfig struct{}

// Module is the e-commerce dashboard's nucleus module. It bundles its
// models for AutoMigrate, its routes (mounted under "/api"), and would
// carry migrations and lifecycle hooks if it had any.
//
// Mounting is a single call in main.go:
//
//	nucleus.New().FromStruct(...).Mount(Module.Build()).Start()
var Module = nucleus.Module[EcommerceConfig]{
	Name:      "ecommerce",
	Prefix:    "/api",
	DefaultDB: "default",
	Models: []any{
		&models.Product{},
		&models.Category{},
		&models.Order{},
		&models.OrderItem{},
		&models.Customer{},
	},
	Routes: func(r nucleus.Router, _ EcommerceConfig) {
		r.Get("/stats", handlers.GetStats)
		r.Get("/products", handlers.ListProducts)
		r.Post("/products", handlers.CreateProduct)
		r.Get("/products/{id}", handlers.GetProduct)
		r.Get("/orders", handlers.ListOrders)
		r.Post("/orders", handlers.CreateOrder)
		r.Get("/customers", handlers.ListCustomers)
		r.Get("/customers/{id}", handlers.GetCustomer)
		r.Get("/categories", handlers.ListCategories)
	},
}
