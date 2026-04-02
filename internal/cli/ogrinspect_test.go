package cli

import "testing"

func TestIsGeometryDBType(t *testing.T) {
	cases := []struct {
		dbType string
		want   bool
	}{
		{dbType: "GEOMETRY", want: true},
		{dbType: "geography(Point,4326)", want: true},
		{dbType: "MULTIPOLYGON", want: true},
		{dbType: "LINESTRING", want: true},
		{dbType: "varchar", want: false},
		{dbType: "timestamp", want: false},
		{dbType: "", want: false},
	}

	for _, tc := range cases {
		got := isGeometryDBType(tc.dbType)
		if got != tc.want {
			t.Fatalf("isGeometryDBType(%q) = %v, want %v", tc.dbType, got, tc.want)
		}
	}
}

func TestHasGeometryColumn(t *testing.T) {
	withGeom := []introspectedColumn{
		{Name: "id", DBType: "INTEGER"},
		{Name: "geom", DBType: "GEOMETRY"},
	}
	if !hasGeometryColumn(withGeom) {
		t.Fatal("expected geometry column detection")
	}

	withoutGeom := []introspectedColumn{
		{Name: "id", DBType: "INTEGER"},
		{Name: "name", DBType: "TEXT"},
	}
	if hasGeometryColumn(withoutGeom) {
		t.Fatal("expected no geometry column detection")
	}
}
