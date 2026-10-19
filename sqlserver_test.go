package sqlserver

import (
	"strings"
	"testing"
)

func TestParseSQLServerVersion(t *testing.T) {
	for _, tc := range []struct {
		name    string
		version string
		want    int
		wantOK  bool
	}{
		{name: "server 2008", version: "10.50.6000.34", want: 10, wantOK: true},
		{name: "server 2012", version: "11.0.7001.0", want: 11, wantOK: true},
		{name: "server 2016", version: "13.0.5026.0", want: 13, wantOK: true},
		{name: "server 2019", version: "15.0.2000.5", want: 15, wantOK: true},
		{name: "empty", version: "", want: 0, wantOK: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseSQLServerVersion(tc.version)
			if ok != tc.wantOK {
				t.Fatalf("parseSQLServerVersion(%q) ok = %v, want %v", tc.version, ok, tc.wantOK)
			}
			if got != tc.want {
				t.Fatalf("parseSQLServerVersion(%q) = %d, want %d", tc.version, got, tc.want)
			}
		})
	}
}

func TestAddTopClause(t *testing.T) {
	query := "SELECT * FROM users WHERE active = 1 ORDER BY id"
	got := addTopClause(query, 10)
	if !strings.Contains(got, "SELECT TOP (10) *") {
		t.Fatalf("addTopClause result = %q, want TOP(10) in SELECT", got)
	}
}

func TestBuildLegacyPaginationSQL(t *testing.T) {
	t.Run("offset zero", func(t *testing.T) {
		limit := 10
		got := buildLegacyPaginationSQL("SELECT * FROM users WHERE active = 1 ORDER BY id", 0, &limit)
		if !strings.Contains(got, "SELECT TOP (10)") {
			t.Fatalf("legacy query without offset = %q, want TOP clause", got)
		}
	})

	t.Run("offset greater than zero", func(t *testing.T) {
		limit := 10
		got := buildLegacyPaginationSQL("SELECT * FROM users WHERE active = 1 ORDER BY id", 20, &limit)
		if !strings.Contains(got, "ROW_NUMBER() OVER (ORDER BY id)") {
			t.Fatalf("legacy query with offset = %q, want ROW_NUMBER() wrap", got)
		}
		if !strings.Contains(got, "__gorm_rownum > 20") {
			t.Fatalf("legacy query with offset = %q, want offset filter applied", got)
		}
		if !strings.Contains(got, "__gorm_rownum <= 30") {
			t.Fatalf("legacy query with offset = %q, want upper bound applied", got)
		}
	})
}
