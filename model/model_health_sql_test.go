package model

import (
	"strings"
	"testing"
)

func TestConflictValueExprForDialect(t *testing.T) {
	tests := []struct {
		name    string
		dialect string
		column  string
		want    string
	}{
		{name: "postgres", dialect: "postgres", column: "total_requests", want: "EXCLUDED.total_requests"},
		{name: "mysql", dialect: "mysql", column: "total_requests", want: "VALUES(total_requests)"},
		{name: "sqlite", dialect: "sqlite", column: "total_requests", want: "VALUES(total_requests)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := conflictValueExprForDialect(tt.dialect, tt.column); got != tt.want {
				t.Fatalf("conflictValueExprForDialect(%q, %q) = %q, want %q", tt.dialect, tt.column, got, tt.want)
			}
		})
	}
}

func TestHourStartExprSQLForDialect(t *testing.T) {
	tests := []struct {
		name    string
		dialect string
		want    string
	}{
		{name: "postgres", dialect: "postgres", want: "((slice_start_ts / 3600) * 3600)"},
		{name: "mysql", dialect: "mysql", want: "((slice_start_ts DIV 3600) * 3600)"},
		{name: "sqlite", dialect: "sqlite", want: "(CAST((slice_start_ts / 3600) AS INTEGER) * 3600)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hourStartExprSQLForDialect(tt.dialect); got != tt.want {
				t.Fatalf("hourStartExprSQLForDialect(%q) = %q, want %q", tt.dialect, got, tt.want)
			}
		})
	}
}

func TestSuccessRateExprSQLUsesBooleanSafeAggregation(t *testing.T) {
	expr := successRateExprSQL()
	if !strings.Contains(expr, "CASE WHEN has_success_qualified THEN 1 ELSE 0 END") {
		t.Fatalf("successRateExprSQL should use CASE aggregation, got %q", expr)
	}
	if strings.Contains(expr, "SUM(has_success_qualified)") {
		t.Fatalf("successRateExprSQL should not sum boolean directly, got %q", expr)
	}
}

func TestFingerprintSearchWhereClauseIsCaseInsensitiveCrossDB(t *testing.T) {
	clause := fingerprintSearchWhereClause()
	if strings.Contains(clause, "ILIKE") {
		t.Fatalf("fingerprintSearchWhereClause should avoid postgres-only ILIKE, got %q", clause)
	}
	if strings.Count(clause, "LOWER(") != 6 {
		t.Fatalf("fingerprintSearchWhereClause should lowercase both sides for three fields, got %q", clause)
	}
}
