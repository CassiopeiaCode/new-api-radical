package model

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	modelHealthSliceSeconds = int64(300)
)

type ModelHealthSlice5m struct {
	SliceStartTs             int64  `json:"slice_start_ts" gorm:"primaryKey;autoIncrement:false;index:idx_slice_start;index:idx_slice_model,priority:1;comment:slice start unix seconds, aligned to 300s"`
	ModelName                string `json:"model_name" gorm:"size:64;primaryKey;autoIncrement:false;default:'';index:idx_slice_model,priority:2;comment:origin model name (after mapping: log model)"`
	TotalRequests            int    `json:"total_requests" gorm:"not null;default:0;comment:events observed in this slice for this model"`
	ErrorRequests            int    `json:"error_requests" gorm:"not null;default:0;comment:events considered failure in this slice for this model"`
	SuccessQualifiedRequests int    `json:"success_qualified_requests" gorm:"not null;default:0;comment:successful requests meeting threshold"`
	HasSuccessQualified      bool   `json:"has_success_qualified" gorm:"not null;default:false;comment:1 if any qualified success in slice"`
	MaxResponseBytes         int    `json:"max_response_bytes" gorm:"not null;default:0;comment:max response bytes observed in slice (0 if unknown)"`
	MaxCompletionTokens      int    `json:"max_completion_tokens" gorm:"not null;default:0;comment:max completion tokens observed in slice"`
	MaxAssistantChars        int    `json:"max_assistant_chars" gorm:"not null;default:0;comment:max assistant content char length observed in slice (0 if unknown)"`
	UpdatedAt                time.Time
}

func (ModelHealthSlice5m) TableName() string {
	return "model_health_slice_5m"
}

type ModelHealthEvent struct {
	ModelName           string
	CreatedAt           int64
	IsError             bool
	ResponseBytes       int
	CompletionTokens    int
	AssistantChars      int
	SuccessIsQualified  bool
	HasMetricsAvailable bool
}

func AlignSliceStartTs(createdAt int64) int64 {
	return createdAt - (createdAt % modelHealthSliceSeconds)
}

func IsQualifiedSuccess(responseBytes, completionTokens, assistantChars int) bool {
	return responseBytes > 1024 || completionTokens > 2 || assistantChars > 2
}

func (e *ModelHealthEvent) Normalize() error {
	if e == nil {
		return errors.New("event is nil")
	}
	if e.ModelName == "" {
		return errors.New("model_name is required")
	}
	if e.CreatedAt <= 0 {
		return errors.New("created_at must be positive")
	}
	if e.ResponseBytes < 0 || e.CompletionTokens < 0 || e.AssistantChars < 0 {
		return errors.New("metrics must be non-negative")
	}
	e.SuccessIsQualified = !e.IsError && IsQualifiedSuccess(e.ResponseBytes, e.CompletionTokens, e.AssistantChars)
	e.HasMetricsAvailable = e.ResponseBytes > 0 || e.CompletionTokens > 0 || e.AssistantChars > 0
	return nil
}

func UpsertModelHealthSlice5m(ctx context.Context, db *gorm.DB, event *ModelHealthEvent) error {
	if event == nil {
		return errors.New("event is nil")
	}
	if err := event.Normalize(); err != nil {
		return err
	}
	if db == nil {
		return errors.New("db is nil")
	}

	sliceStart := AlignSliceStartTs(event.CreatedAt)

	row := &ModelHealthSlice5m{
		SliceStartTs:             sliceStart,
		ModelName:                event.ModelName,
		TotalRequests:            1,
		ErrorRequests:            0,
		SuccessQualifiedRequests: 0,
		HasSuccessQualified:      event.SuccessIsQualified,
		MaxResponseBytes:         maxInt(0, event.ResponseBytes),
		MaxCompletionTokens:      maxInt(0, event.CompletionTokens),
		MaxAssistantChars:        maxInt(0, event.AssistantChars),
	}

	if event.IsError {
		row.ErrorRequests = 1
	}
	if event.SuccessIsQualified {
		row.SuccessQualifiedRequests = 1
	}

	updates := map[string]any{
		"total_requests":             gorm.Expr(fmt.Sprintf("total_requests + %s", conflictValueExpr(db, "total_requests"))),
		"error_requests":             gorm.Expr(fmt.Sprintf("error_requests + %s", conflictValueExpr(db, "error_requests"))),
		"success_qualified_requests": gorm.Expr(fmt.Sprintf("success_qualified_requests + %s", conflictValueExpr(db, "success_qualified_requests"))),
		"has_success_qualified":      gorm.Expr(fmt.Sprintf("has_success_qualified OR %s", conflictValueExpr(db, "has_success_qualified"))),
		"max_response_bytes":         gorm.Expr(fmt.Sprintf("GREATEST(max_response_bytes, %s)", conflictValueExpr(db, "max_response_bytes"))),
		"max_completion_tokens":      gorm.Expr(fmt.Sprintf("GREATEST(max_completion_tokens, %s)", conflictValueExpr(db, "max_completion_tokens"))),
		"max_assistant_chars":        gorm.Expr(fmt.Sprintf("GREATEST(max_assistant_chars, %s)", conflictValueExpr(db, "max_assistant_chars"))),
	}

	return db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "model_name"},
			{Name: "slice_start_ts"},
		},
		DoUpdates: clause.Assignments(updates),
	}).Create(row).Error
}

func conflictValueExpr(db *gorm.DB, column string) string {
	return conflictValueExprForDialect(dbDialectName(db), column)
}

func conflictValueExprForDialect(dialectName string, column string) string {
	if dialectName == "postgres" {
		return fmt.Sprintf("EXCLUDED.%s", column)
	}
	return fmt.Sprintf("VALUES(%s)", column)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
