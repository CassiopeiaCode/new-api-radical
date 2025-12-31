package model

import (
	"context"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/bytedance/gopkg/util/gopool"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	// 内存聚合刷新间隔
	userCallHourlyFlushInterval = 5 * time.Second
	// 内存聚合最大条目数，超过则强制刷新
	userCallHourlyMaxBufferSize = 1000
)

type UserCallHourlyEvent struct {
	UserId    int
	Username  string
	CreatedAt int64
	IsError   bool
}

// userCallHourlyKey 用于内存聚合的 key
type userCallHourlyKey struct {
	HourStartTs int64
	UserId      int
}

// userCallHourlyAgg 内存中聚合的数据
type userCallHourlyAgg struct {
	Username     string
	TotalCalls   int
	SuccessCalls int
}

var (
	userCallHourlyOnce   sync.Once
	userCallHourlyMu     sync.Mutex
	userCallHourlyBuffer map[userCallHourlyKey]*userCallHourlyAgg
)

func AlignHourStartTs(createdAt int64) int64 {
	if createdAt <= 0 {
		return 0
	}
	return createdAt - (createdAt % 3600)
}

// initUserCallHourlyWriter 初始化内存聚合写入器
func initUserCallHourlyWriter() {
	userCallHourlyBuffer = make(map[userCallHourlyKey]*userCallHourlyAgg)

	// 启动定时刷新协程
	gopool.Go(func() {
		ticker := time.NewTicker(userCallHourlyFlushInterval)
		defer ticker.Stop()
		for range ticker.C {
			flushUserCallHourlyBuffer(false)
		}
	})
}


// flushUserCallHourlyBuffer 将内存中聚合的数据批量写入数据库
func flushUserCallHourlyBuffer(force bool) {
	userCallHourlyMu.Lock()
	if len(userCallHourlyBuffer) == 0 {
		userCallHourlyMu.Unlock()
		return
	}

	// 交换 buffer，快速释放锁
	oldBuffer := userCallHourlyBuffer
	userCallHourlyBuffer = make(map[userCallHourlyKey]*userCallHourlyAgg)
	userCallHourlyMu.Unlock()

	// 批量写入数据库
	batchUpsertUserCallHourly(context.Background(), DB, oldBuffer)
}

// batchUpsertUserCallHourly 批量 upsert 到数据库
func batchUpsertUserCallHourly(ctx context.Context, db *gorm.DB, buffer map[userCallHourlyKey]*userCallHourlyAgg) {
	if db == nil || len(buffer) == 0 {
		return
	}

	rows := make([]*UserCallHourly, 0, len(buffer))
	for key, agg := range buffer {
		rows = append(rows, &UserCallHourly{
			HourStartTs:  key.HourStartTs,
			UserId:       key.UserId,
			Username:     agg.Username,
			TotalCalls:   agg.TotalCalls,
			SuccessCalls: agg.SuccessCalls,
		})
	}

	// 根据数据库类型选择不同的 upsert 语法
	var doUpdates clause.Set
	if common.UsingPostgreSQL {
		// PostgreSQL: EXCLUDED.column
		doUpdates = clause.Assignments(map[string]any{
			"username":      gorm.Expr("EXCLUDED.username"),
			"total_calls":   gorm.Expr("total_calls + EXCLUDED.total_calls"),
			"success_calls": gorm.Expr("success_calls + EXCLUDED.success_calls"),
		})
	} else {
		// MySQL: VALUES(column)
		doUpdates = clause.Assignments(map[string]any{
			"username":      gorm.Expr("VALUES(username)"),
			"total_calls":   gorm.Expr("total_calls + VALUES(total_calls)"),
			"success_calls": gorm.Expr("success_calls + VALUES(success_calls)"),
		})
	}

	// 分批写入，每批最多100条
	const batchSize = 100
	for i := 0; i < len(rows); i += batchSize {
		end := i + batchSize
		if end > len(rows) {
			end = len(rows)
		}
		batch := rows[i:end]

		func() {
			defer func() { _ = recover() }()
			_ = db.WithContext(ctx).Clauses(clause.OnConflict{
				Columns: []clause.Column{
					{Name: "hour_start_ts"},
					{Name: "user_id"},
				},
				DoUpdates: doUpdates,
			}).Create(&batch).Error
		}()
	}
}

// RecordUserCallHourlyEventAsync 异步记录用户调用事件（内存聚合）
func RecordUserCallHourlyEventAsync(_ any, event *UserCallHourlyEvent) {
	if event == nil {
		return
	}
	userCallHourlyOnce.Do(initUserCallHourlyWriter)

	hourStart := AlignHourStartTs(event.CreatedAt)
	if hourStart == 0 || event.UserId <= 0 {
		return
	}

	key := userCallHourlyKey{
		HourStartTs: hourStart,
		UserId:      event.UserId,
	}

	userCallHourlyMu.Lock()
	agg, exists := userCallHourlyBuffer[key]
	if !exists {
		agg = &userCallHourlyAgg{}
		userCallHourlyBuffer[key] = agg
	}
	agg.Username = event.Username
	agg.TotalCalls++
	if !event.IsError {
		agg.SuccessCalls++
	}
	bufferSize := len(userCallHourlyBuffer)
	userCallHourlyMu.Unlock()

	// 超过最大条目数时异步触发刷新
	if bufferSize >= userCallHourlyMaxBufferSize {
		gopool.Go(func() {
			flushUserCallHourlyBuffer(true)
		})
	}
}

// FlushUserCallHourlyBufferSync 同步刷新缓冲区（用于优雅关闭）
func FlushUserCallHourlyBufferSync() {
	flushUserCallHourlyBuffer(true)
}
