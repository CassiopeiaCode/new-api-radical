package model

import (
	"time"

	"github.com/QuantumNous/new-api/common"
)

// UserFingerprint 用户设备指纹记录
type UserFingerprint struct {
	Id        int       `json:"id" gorm:"primaryKey;autoIncrement"`
	UserId    int       `json:"user_id" gorm:"index;not null"`
	VisitorId string    `json:"visitor_id" gorm:"type:varchar(64);index;not null"`
	UserAgent string    `json:"user_agent" gorm:"type:varchar(512)"`
	IP        string    `json:"ip" gorm:"type:varchar(64)"`
	CreatedAt time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt time.Time `json:"updated_at" gorm:"autoUpdateTime"`
}

func (UserFingerprint) TableName() string {
	return "user_fingerprints"
}

// RecordFingerprint 记录用户指纹，保留最近5个不同的visitor id
func RecordFingerprint(userId int, visitorId string, userAgent string, ip string) error {
	// 检查该用户是否已有此visitor id
	var existing UserFingerprint
	err := DB.Where("user_id = ? AND visitor_id = ?", userId, visitorId).First(&existing).Error

	if err == nil {
		// 已存在，更新时间和IP
		existing.UserAgent = userAgent
		existing.IP = ip
		return DB.Save(&existing).Error
	}

	// 不存在，创建新记录
	fingerprint := UserFingerprint{
		UserId:    userId,
		VisitorId: visitorId,
		UserAgent: userAgent,
		IP:        ip,
	}

	if err := DB.Create(&fingerprint).Error; err != nil {
		return err
	}

	// 统计该用户不同的visitor id数量
	var count int64
	DB.Model(&UserFingerprint{}).Where("user_id = ?", userId).Count(&count)

	// 如果超过5个，删除最旧的记录
	if count > 5 {
		var oldRecords []UserFingerprint
		DB.Where("user_id = ?", userId).Order("updated_at desc").Offset(5).Find(&oldRecords)

		if len(oldRecords) > 0 {
			var ids []int
			for _, r := range oldRecords {
				ids = append(ids, r.Id)
			}
			DB.Delete(&UserFingerprint{}, ids)
		}
	}

	return nil
}

// GetUserFingerprints 获取用户的指纹历史
func GetUserFingerprints(userId int) ([]UserFingerprint, error) {
	var fingerprints []UserFingerprint
	err := DB.Where("user_id = ?", userId).Order("created_at desc").Limit(5).Find(&fingerprints).Error
	return fingerprints, err
}

// UserWithFingerprint 用于返回用户信息和指纹
type UserWithFingerprint struct {
	Id          int    `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
	Status      int    `json:"status"`
	Role        int    `json:"role"`
	VisitorId   string `json:"visitor_id"`
	RecordTime  string `json:"record_time"`
	IP          string `json:"ip"`
}

// FindUsersByVisitorId 查找具有相同visitor id的用户
func FindUsersByVisitorId(visitorId string, pageInfo *common.PageInfo) ([]UserWithFingerprint, int64, error) {
	var results []UserWithFingerprint
	var total int64

	// 先统计总数
	err := DB.Model(&UserFingerprint{}).
		Where("visitor_id = ?", visitorId).
		Select("DISTINCT user_id").
		Count(&total).Error
	if err != nil {
		return nil, 0, err
	}

	// 查询用户信息
	query := `
		SELECT DISTINCT u.id, u.username, u.display_name, u.email, u.status, u.role, 
			   f.visitor_id, f.created_at as record_time, f.ip
		FROM user_fingerprints f
		JOIN users u ON f.user_id = u.id
		WHERE f.visitor_id = ?
		ORDER BY f.created_at DESC
		LIMIT ? OFFSET ?
	`

	err = DB.Raw(query, visitorId, pageInfo.GetPageSize(), pageInfo.GetStartIdx()).Scan(&results).Error
	return results, total, err
}

// GetAllFingerprints 获取所有指纹记录（管理员用）
func GetAllFingerprints(pageInfo *common.PageInfo) ([]UserWithFingerprint, int64, error) {
	var results []UserWithFingerprint
	var total int64

	// 统计总数
	err := DB.Model(&UserFingerprint{}).Count(&total).Error
	if err != nil {
		return nil, 0, err
	}

	// 查询
	query := `
		SELECT u.id, u.username, u.display_name, u.email, u.status, u.role,
			   f.visitor_id, f.created_at as record_time, f.ip
		FROM user_fingerprints f
		JOIN users u ON f.user_id = u.id
		ORDER BY f.created_at DESC
		LIMIT ? OFFSET ?
	`

	err = DB.Raw(query, pageInfo.GetPageSize(), pageInfo.GetStartIdx()).Scan(&results).Error
	return results, total, err
}

// SearchFingerprints 搜索指纹记录
func SearchFingerprints(keyword string, pageInfo *common.PageInfo) ([]UserWithFingerprint, int64, error) {
	var results []UserWithFingerprint
	var total int64

	// 统计总数
	countQuery := `
		SELECT COUNT(*) FROM user_fingerprints f
		JOIN users u ON f.user_id = u.id
		WHERE f.visitor_id LIKE ? OR u.username LIKE ? OR u.email LIKE ?
	`
	likeKeyword := "%" + keyword + "%"
	err := DB.Raw(countQuery, likeKeyword, likeKeyword, likeKeyword).Scan(&total).Error
	if err != nil {
		return nil, 0, err
	}

	// 查询
	query := `
		SELECT u.id, u.username, u.display_name, u.email, u.status, u.role,
			   f.visitor_id, f.created_at as record_time, f.ip
		FROM user_fingerprints f
		JOIN users u ON f.user_id = u.id
		WHERE f.visitor_id LIKE ? OR u.username LIKE ? OR u.email LIKE ?
		ORDER BY f.created_at DESC
		LIMIT ? OFFSET ?
	`

	err = DB.Raw(query, likeKeyword, likeKeyword, likeKeyword, pageInfo.GetPageSize(), pageInfo.GetStartIdx()).Scan(&results).Error
	return results, total, err
}

// GetDuplicateVisitorIds 获取有多个用户使用的visitor id列表
func GetDuplicateVisitorIds(pageInfo *common.PageInfo) ([]map[string]interface{}, int64, error) {
	var total int64

	// 统计有重复的visitor_id数量
	countQuery := `
		SELECT COUNT(*) FROM (
			SELECT visitor_id FROM user_fingerprints 
			GROUP BY visitor_id 
			HAVING COUNT(DISTINCT user_id) > 1
		) AS duplicates
	`
	err := DB.Raw(countQuery).Scan(&total).Error
	if err != nil {
		return nil, 0, err
	}

	// 查询重复的visitor_id及其用户数
	query := `
		SELECT visitor_id, COUNT(DISTINCT user_id) as user_count, MAX(created_at) as last_seen
		FROM user_fingerprints
		GROUP BY visitor_id
		HAVING COUNT(DISTINCT user_id) > 1
		ORDER BY user_count DESC, last_seen DESC
		LIMIT ? OFFSET ?
	`

	var results []struct {
		VisitorId string    `json:"visitor_id"`
		UserCount int       `json:"user_count"`
		LastSeen  time.Time `json:"last_seen"`
	}

	err = DB.Raw(query, pageInfo.GetPageSize(), pageInfo.GetStartIdx()).Scan(&results).Error
	if err != nil {
		return nil, 0, err
	}

	// 转换为map格式
	var mapResults []map[string]interface{}
	for _, r := range results {
		mapResults = append(mapResults, map[string]interface{}{
			"visitor_id": r.VisitorId,
			"user_count": r.UserCount,
			"last_seen":  r.LastSeen,
		})
	}

	return mapResults, total, nil
}
