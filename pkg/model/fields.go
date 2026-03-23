package model

import (
	"strings"
)

// FieldMeta holds all metadata extracted from a single struct field.
type FieldMeta struct {
	Name         string   // Go field name (e.g. "Email")
	Column       string   // SQL column name (e.g. "email")
	Label        string   // Human-readable label (e.g. "Correo electrónico")
	GoType       string   // Go type as string (e.g. "string", "int", "bool")
	HTMLType     string   // HTML input type (e.g. "text", "email", "number")
	IsPK         bool     // Is primary key
	IsRequired   bool     // Required field (not null / validate:"required")
	IsReadOnly   bool     // Read-only in admin forms
	IsList       bool     // Shown in list view
	IsSearch     bool     // Included in search queries
	IsFilter     bool     // Shown as filter option
	IsExcluded   bool     // Excluded from admin entirely
	IsForeignKey bool     // This field is a foreign key reference
	ForeignModel string   // Name of the related model (e.g. "User" for UserID)
	MaxLength    int      // Max length from validate tag
	Choices      []Choice // Enum/select options
}

// Choice represents a selectable option for enum/select fields.
type Choice struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// inferHTMLType maps a Go type and field name to an appropriate HTML input type.
func inferHTMLType(goType, fieldName string) string {
	lower := strings.ToLower(fieldName)

	// Name-based inference takes priority
	switch {
	case strings.Contains(lower, "email"):
		return "email"
	case strings.Contains(lower, "password"):
		return "password"
	case strings.Contains(lower, "url") || strings.Contains(lower, "link") || strings.Contains(lower, "website"):
		return "url"
	case strings.Contains(lower, "phone") || strings.Contains(lower, "tel"):
		return "tel"
	case strings.Contains(lower, "description") || strings.Contains(lower, "body") || strings.Contains(lower, "content") || strings.Contains(lower, "bio") || strings.Contains(lower, "notes"):
		return "textarea"
	case strings.Contains(lower, "color"):
		return "color"
	}

	// Type-based inference
	switch goType {
	case "string":
		return "text"
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64":
		return "number"
	case "float32", "float64":
		return "number"
	case "bool":
		return "checkbox"
	case "time.Time", "Time":
		return "datetime-local"
	default:
		return "text"
	}
}

// adminTagOpts holds the parsed values from an `admin:` struct tag.
type adminTagOpts struct {
	IsList     bool
	IsSearch   bool
	IsFilter   bool
	IsReadOnly bool
	IsExcluded bool
	Label      string
	Choices    []Choice
}

// parseAdminTag parses a comma-separated admin tag value.
// Supported directives: list, search, filter, readonly, exclude, label:Value,
// choices:val1|Label1;val2|Label2
func parseAdminTag(tag string) adminTagOpts {
	var opts adminTagOpts
	if tag == "" || tag == "-" {
		if tag == "-" {
			opts.IsExcluded = true
		}
		return opts
	}

	parts := strings.Split(tag, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		switch {
		case p == "list":
			opts.IsList = true
		case p == "search":
			opts.IsSearch = true
		case p == "filter":
			opts.IsFilter = true
		case p == "readonly":
			opts.IsReadOnly = true
		case p == "exclude":
			opts.IsExcluded = true
		case strings.HasPrefix(p, "label:"):
			opts.Label = strings.TrimPrefix(p, "label:")
		case strings.HasPrefix(p, "choices:"):
			raw := strings.TrimPrefix(p, "choices:")
			for _, c := range strings.Split(raw, ";") {
				parts := strings.SplitN(c, "|", 2)
				choice := Choice{Value: parts[0]}
				if len(parts) == 2 {
					choice.Label = parts[1]
				} else {
					choice.Label = parts[0]
				}
				opts.Choices = append(opts.Choices, choice)
			}
		}
	}

	return opts
}

// toTitle converts a CamelCase or snake_case name to a human-readable title.
// Examples: "CreatedAt" -> "Created At", "user_name" -> "User Name"
func toTitle(name string) string {
	if name == "" {
		return ""
	}

	// Handle snake_case
	if strings.Contains(name, "_") {
		parts := strings.Split(name, "_")
		for i, p := range parts {
			if len(p) > 0 {
				parts[i] = strings.ToUpper(p[:1]) + p[1:]
			}
		}
		return strings.Join(parts, " ")
	}

	// Handle CamelCase
	var result strings.Builder
	for i, r := range name {
		if i > 0 && r >= 'A' && r <= 'Z' {
			result.WriteByte(' ')
		}
		result.WriteRune(r)
	}
	return result.String()
}

// toSnakeCase converts CamelCase to snake_case.
func toSnakeCase(s string) string {
	var result strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			result.WriteByte('_')
		}
		result.WriteRune(r)
	}
	return strings.ToLower(result.String())
}

// toPlural applies simple English pluralization rules.
func toPlural(s string) string {
	if s == "" {
		return ""
	}
	lower := strings.ToLower(s)
	switch {
	case strings.HasSuffix(lower, "s") || strings.HasSuffix(lower, "x") ||
		strings.HasSuffix(lower, "z") || strings.HasSuffix(lower, "ch") ||
		strings.HasSuffix(lower, "sh"):
		return s + "es"
	case strings.HasSuffix(lower, "y") && len(s) > 1 && !isVowel(rune(lower[len(lower)-2])):
		return s[:len(s)-1] + "ies"
	default:
		return s + "s"
	}
}

func isVowel(r rune) bool {
	switch r {
	case 'a', 'e', 'i', 'o', 'u':
		return true
	}
	return false
}
