package quark

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/jcsvwinston/GoFrame/pkg/quark/internal/migrate"
)

// Migrate creates tables for the given models if they don't exist.
// This is a simplistic auto-migration tool for development.
// It uses the "db" and "pk" tags to generate CREATE TABLE statements.
func (c *Client) Migrate(ctx context.Context, models ...any) error {
	for _, model := range models {
		if err := c.createTable(ctx, model); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) createTable(ctx context.Context, model any) error {
	t := reflect.TypeOf(model)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	if t.Kind() != reflect.Struct {
		return fmt.Errorf("model must be a struct, got %s", t.Kind())
	}

	tableName := toSnakeCase(pluralize(t.Name()))

	var columns []string

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		dbTag := field.Tag.Get("db")
		if dbTag == "" || dbTag == "-" {
			continue
		}

		pkTag := field.Tag.Get("pk") == "true"
		colDef := c.dialect.Quote(dbTag) + " " + migrate.SQLType(c.dialect.Name(), field.Type, pkTag)
		columns = append(columns, colDef)
	}

	if len(columns) == 0 {
		return fmt.Errorf("no database columns found for model %s", t.Name())
	}

	query := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n  %s\n);", 
		c.dialect.Quote(tableName), 
		strings.Join(columns, ",\n  "),
	)

	_, err := c.db.ExecContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to create table %s: %w", tableName, err)
	}

	return nil
}
