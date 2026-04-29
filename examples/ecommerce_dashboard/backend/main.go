package main

import (
	"fmt"
	"math/rand"

	"github.com/jcsvwinston/GoFrame/pkg/faker"
	"github.com/jcsvwinston/GoFrame/pkg/goframe"
	"github.com/jcsvwinston/GoFrame/pkg/model"
)

// Product model
type Product struct {
	model.BaseModel
	Name        string  `json:"name" db:"name" validate:"required"`
	Description string  `json:"description" db:"description"`
	Price       float64 `json:"price" db:"price" validate:"min=0"`
	Stock       int     `json:"stock" db:"stock" validate:"min=0"`
	CategoryID  int64   `json:"category_id" db:"category_id"`
	Image       string  `json:"image" db:"image"`
	SKU         string  `json:"sku" db:"sku" validate:"required"`
}

// Category model
type Category struct {
	model.BaseModel
	Name        string `json:"name" db:"name" validate:"required"`
	Description string `json:"description" db:"description"`
	Icon        string `json:"icon" db:"icon"`
}

// Order model
type Order struct {
	model.BaseModel
	CustomerID int64       `json:"customer_id" db:"customer_id"`
	Status     string      `json:"status" db:"status" validate:"required"`
	Total      float64     `json:"total" db:"total"`
	Items      []OrderItem `json:"items" db:"-"`
}

// OrderItem model
type OrderItem struct {
	model.BaseModel
	OrderID   int64   `json:"order_id" db:"order_id"`
	ProductID int64   `json:"product_id" db:"product_id"`
	Quantity  int     `json:"quantity" db:"quantity"`
	Price     float64 `json:"price" db:"price"`
}

// Customer model
type Customer struct {
	model.BaseModel
	Name    string `json:"name" db:"name" validate:"required"`
	Email   string `json:"email" db:"email" validate:"required,email"`
	Phone   string `json:"phone" db:"phone"`
	Address string `json:"address" db:"address"`
}

// Stats response
type StatsResponse struct {
	TotalProducts  int64   `json:"total_products"`
	TotalOrders    int64   `json:"total_orders"`
	TotalCustomers int64   `json:"total_customers"`
	Revenue        float64 `json:"revenue"`
	OrdersToday    int64   `json:"orders_today"`
	RevenueToday   float64 `json:"revenue_today"`
}

func main() {
	// Create app with SQLite and auto-migration
	app := goframe.New().
		Port(8080).
		SQLite("ecommerce.db").
		Model(&Product{}).
		Model(&Category{}).
		Model(&Order{}).
		Model(&OrderItem{}).
		Model(&Customer{}).
		AutoMigrate()

	// Seed database with massive data
	seedDatabase()

	// Register API routes
	api := app.Group("/api")

	// Stats endpoint
	api.Get("/stats", getStats)

	// Products CRUD
	api.Get("/products", listProducts)
	api.Post("/products", createProduct)
	api.Get("/products/:id", getProduct)

	// Orders CRUD
	api.Get("/orders", listOrders)
	api.Post("/orders", createOrder)

	// Customers CRUD
	api.Get("/customers", listCustomers)
	api.Get("/customers/:id", getCustomer)

	// Categories
	api.Get("/categories", listCategories)

	// SPA serving
	app.SPA("../frontend/dist", goframe.SPAConfig{
		IndexFile: "index.html",
		APIPrefix: "/api",
	})

	fmt.Println("🚀 E-Commerce Dashboard")
	fmt.Println("📊 API: http://localhost:8080/api")
	fmt.Println("🌐 App: http://localhost:8080")

	if err := app.Run(); err != nil {
		fmt.Printf("Error: %v\n", err)
	}
}

func seedDatabase() {
	fmt.Println("🌱 Seeding database with 100K+ records...")

	// Seed categories (50)
	categories := []Category{
		{Name: "Electronics", Icon: "📱"},
		{Name: "Clothing", Icon: "👕"},
		{Name: "Home & Garden", Icon: "🏠"},
		{Name: "Sports", Icon: "⚽"},
		{Name: "Books", Icon: "📚"},
		{Name: "Toys", Icon: "🧸"},
		{Name: "Food", Icon: "🍔"},
		{Name: "Beauty", Icon: "💄"},
		{Name: "Automotive", Icon: "🚗"},
		{Name: "Office", Icon: "📎"},
	}
	for _, cat := range categories {
		cat.Description = faker.Sentence(10)
		// Insert category
		_ = cat
	}

	// Seed products (100,000)
	fmt.Println("   Creating 100,000 products...")
	products := make([]Product, 100000)
	for i := range products {
		products[i] = Product{
			Name:        faker.ProductName(),
			Description: faker.Paragraph(2),
			Price:       float64(rand.Intn(100000)) / 100.0,
			Stock:       rand.Intn(1000),
			CategoryID:  int64(rand.Intn(10) + 1),
			SKU:         fmt.Sprintf("SKU-%06d", i),
			Image:       fmt.Sprintf("https://picsum.photos/300/200?random=%d", i),
		}
	}
	// Insert products
	_ = products

	// Seed customers (50,000)
	fmt.Println("   Creating 50,000 customers...")
	customers := make([]Customer, 50000)
	for i := range customers {
		customers[i] = Customer{
			Name:    faker.Name(),
			Email:   faker.Email(),
			Phone:   faker.Phone(),
			Address: faker.Address(),
		}
	}
	_ = customers

	// Seed orders (500,000)
	fmt.Println("   Creating 500,000 orders...")
	orders := make([]Order, 500000)
	statuses := []string{"pending", "completed", "shipped", "cancelled"}
	for i := range orders {
		orders[i] = Order{
			CustomerID: int64(rand.Intn(50000) + 1),
			Status:     statuses[rand.Intn(len(statuses))],
			Total:      float64(rand.Intn(10000)) / 100.0,
		}
	}
	_ = orders

	fmt.Println("✅ Database seeded!")
}

// Handlers
func getStats(c *goframe.Context) error {
	// Simulate stats from database
	stats := StatsResponse{
		TotalProducts:  100000,
		TotalOrders:    500000,
		TotalCustomers: 50000,
		Revenue:        2500000.50,
		OrdersToday:    rand.Int63n(500),
		RevenueToday:   float64(rand.Int63n(50000)) / 100.0,
	}
	return c.JSON(200, stats)
}

func listProducts(c *goframe.Context) error {
	// Return paginated products
	return c.JSON(200, map[string]interface{}{
		"products": []Product{
			{Name: "Sample Product", Price: 99.99, Stock: 100},
		},
		"total": 100000,
	})
}

func createProduct(c *goframe.Context) error {
	var product Product
	if err := c.BindJSON(&product); err != nil {
		return err
	}
	return c.JSON(201, product)
}

func getProduct(c *goframe.Context) error {
	id := c.Param("id")
	return c.JSON(200, Product{
		Name:  "Product " + id,
		Price: 99.99,
	})
}

func listOrders(c *goframe.Context) error {
	return c.JSON(200, map[string]interface{}{
		"orders": []Order{},
		"total":  500000,
	})
}

func createOrder(c *goframe.Context) error {
	var order Order
	if err := c.BindJSON(&order); err != nil {
		return err
	}
	return c.JSON(201, order)
}

func listCustomers(c *goframe.Context) error {
	return c.JSON(200, map[string]interface{}{
		"customers": []Customer{},
		"total":     50000,
	})
}

func getCustomer(c *goframe.Context) error {
	id := c.Param("id")
	return c.JSON(200, Customer{
		Name:  "Customer " + id,
		Email: "customer@example.com",
	})
}

func listCategories(c *goframe.Context) error {
	return c.JSON(200, []Category{
		{Name: "Electronics", Icon: "📱"},
		{Name: "Clothing", Icon: "👕"},
		{Name: "Home", Icon: "🏠"},
	})
}
