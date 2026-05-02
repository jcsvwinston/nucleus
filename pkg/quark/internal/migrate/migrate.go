// Package migrate provides internal utilities for database schema migrations.
package migrate

import (
	"reflect"
)

// SQLType maps Go types to SQL types for the given dialect name.
func SQLType(dialectName string, t reflect.Type, isPK bool) string {
	if isPK {
		switch dialectName {
		case "sqlite":
			return "INTEGER PRIMARY KEY AUTOINCREMENT"
		case "postgres":
			return "SERIAL PRIMARY KEY"
		case "mysql":
			return "INT AUTO_INCREMENT PRIMARY KEY"
		default:
			return "INTEGER PRIMARY KEY"
		}
	}

	switch t.Kind() {
	case reflect.String:
		if dialectName == "postgres" || dialectName == "sqlite" {
			return "TEXT"
		}
		return "VARCHAR(255)"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "INTEGER"
	case reflect.Float32, reflect.Float64:
		switch dialectName {
		case "sqlite", "postgres":
			return "REAL"
		case "mysql":
			return "DOUBLE"
		default:
			return "REAL"
		}
	case reflect.Bool:
		return "BOOLEAN"
	case reflect.Struct:
		if t.String() == "time.Time" {
			switch dialectName {
			case "sqlite", "mysql":
				return "DATETIME"
			case "postgres":
				return "TIMESTAMP"
			default:
				return "TIMESTAMP"
			}
		}
	}

	return "TEXT" // Fallback
}
