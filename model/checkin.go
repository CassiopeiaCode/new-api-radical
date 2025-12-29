package model

import (
	"time"
)

// Checkin 签到记录表
type Checkin struct {
	Id          int       `json:"id" gorm:"primaryKey;autoIncrement"`
	UserId      int       `json:"user_id" gorm:"not null;uniqueIndex:idx_user_checkin_date"`
	Quota       int       `json:"quota" gorm:"not null"`
	CheckinDate string    `json:"checkin_date" gorm:"type:varchar(10);not null;uniqueIndex:idx_user_checkin_date"` // 格式: 2025-01-01
	CreatedAt   time.Time `json:"created_at"`
}

func (Checkin) TableName() string {
	return "checkins"
}

// GetTodayDateUTC8 获取 UTC+8 时区的今天日期
func GetTodayDateUTC8() string {
	loc := time.FixedZone("UTC+8", 8*60*60)
	return time.Now().In(loc).Format("2006-01-02")
}

// HasCheckedInToday 检查用户今天是否已签到
func HasCheckedInToday(userId int) (bool, error) {
	var count int64
	err := DB.Model(&Checkin{}).
		Where("user_id = ? AND checkin_date = ?", userId, GetTodayDateUTC8()).
		Count(&count).Error
	return count > 0, err
}

// CreateCheckinRecord 创建签到记录
func CreateCheckinRecord(userId int, quota int) error {
	checkin := Checkin{
		UserId:      userId,
		Quota:       quota,
		CheckinDate: GetTodayDateUTC8(),
		CreatedAt:   time.Now(),
	}
	return DB.Create(&checkin).Error
}

// GetUserCheckinHistory 获取用户签到历史
func GetUserCheckinHistory(userId int, limit int) ([]Checkin, error) {
	var records []Checkin
	err := DB.Where("user_id = ?", userId).
		Order("created_at DESC").
		Limit(limit).
		Find(&records).Error
	return records, err
}

// GetUserCheckinCount 获取用户签到总次数
func GetUserCheckinCount(userId int) (int64, error) {
	var count int64
	err := DB.Model(&Checkin{}).Where("user_id = ?", userId).Count(&count).Error
	return count, err
}

// GetUserTotalCheckinQuota 获取用户签到获得的总额度
func GetUserTotalCheckinQuota(userId int) (int64, error) {
	var total int64
	err := DB.Model(&Checkin{}).
		Where("user_id = ?", userId).
		Select("COALESCE(SUM(quota), 0)").
		Scan(&total).Error
	return total, err
}
