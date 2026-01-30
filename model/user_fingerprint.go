package model

import (
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm/clause"
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

// RecordFingerprint 记录用户指纹，按 (user_id, visitor_id, ip) 组合去重，保留最近5条组合记录
func RecordFingerprint(userId int, visitorId string, userAgent string, ip string) error {
	now := time.Now()

	fingerprint := UserFingerprint{
		UserId:    userId,
		VisitorId: visitorId,
		UserAgent: userAgent,
		IP:        ip,
		UpdatedAt: now,
	}

	// 依赖 DB 侧唯一约束：UNIQUE(user_id, visitor_id, ip)
	// 命中则更新 user_agent/updated_at；created_at 保持首次插入时间
	if err := DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "user_id"},
			{Name: "visitor_id"},
			{Name: "ip"},
		},
		DoUpdates: clause.AssignmentColumns([]string{"user_agent", "updated_at"}),
	}).Create(&fingerprint).Error; err != nil {
		return err
	}

	// 统计该用户组合记录数量（唯一约束存在时，行数即组合数）
	var count int64
	DB.Model(&UserFingerprint{}).Where("user_id = ?", userId).Count(&count)

	// 如果超过5条，删除最旧的记录
	if count > 5 {
		var oldRecords []UserFingerprint
		DB.Select("id").Where("user_id = ?", userId).Order("updated_at desc, id desc").Offset(5).Find(&oldRecords)

		if len(oldRecords) > 0 {
			ids := make([]int, 0, len(oldRecords))
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
	err := DB.Where("user_id = ?", userId).Order("updated_at desc, id desc").Limit(5).Find(&fingerprints).Error
	return fingerprints, err
}

// UserWithFingerprint 用于返回用户信息和指纹
type UserWithFingerprint struct {
	Id           int    `json:"id"`
	Username     string `json:"username"`
	DisplayName  string `json:"display_name"`
	Email        string `json:"email"`
	Status       int    `json:"status"`
	Role         int    `json:"role"`
	Quota        int    `json:"quota"`
	UsedQuota    int    `json:"used_quota"`
	RequestCount int    `json:"request_count"`
	VisitorId    string `json:"visitor_id"`
	RecordTime   string `json:"record_time"`
	IP           string `json:"ip"`
}

func FindUsersByVisitorId(visitorId string, ip string, pageInfo *common.PageInfo) ([]UserWithFingerprint, int64, error) {
	var results []UserWithFingerprint
	var total int64

	// 构建基础查询
	baseWhere := "f.visitor_id = ?"
	args := []interface{}{visitorId}
	if ip != "" {
		baseWhere += " AND f.ip = ?"
		args = append(args, ip)
	}

	// 统计不同用户数
	countQuery := `SELECT COUNT(DISTINCT f.user_id) FROM user_fingerprints f WHERE ` + baseWhere
	err := DB.Raw(countQuery, args...).Scan(&total).Error
	if err != nil {
		return nil, 0, err
	}

	// 查询用户信息，使用子查询确保每个用户只返回一条记录
	query := `
		SELECT u.id, u.username, u.display_name, u.email, u.status, u.role,
			   u.quota, u.used_quota, u.request_count,
			   f.visitor_id, f.created_at as record_time, f.ip
		FROM user_fingerprints f
		JOIN users u ON f.user_id = u.id
		WHERE ` + baseWhere + `
		AND f.id IN (
			SELECT MAX(f2.id) FROM user_fingerprints f2
			WHERE f2.visitor_id = ?` + func() string {
		if ip != "" {
			return " AND f2.ip = ?"
		}
		return ""
	}() + ` GROUP BY f2.user_id
		)
		ORDER BY f.created_at DESC
		LIMIT ? OFFSET ?
	`

	// 构建查询参数
	queryArgs := make([]interface{}, 0, len(args)*2+2)
	queryArgs = append(queryArgs, args...)
	queryArgs = append(queryArgs, visitorId)
	if ip != "" {
		queryArgs = append(queryArgs, ip)
	}
	queryArgs = append(queryArgs, pageInfo.GetPageSize(), pageInfo.GetStartIdx())

	err = DB.Raw(query, queryArgs...).Scan(&results).Error
	return results, total, err
}

// FindUsersByIP 查找具有相同IP的用户（管理员）
func FindUsersByIP(ip string, pageInfo *common.PageInfo) ([]UserWithFingerprint, int64, error) {
	var results []UserWithFingerprint
	var total int64

	baseWhere := "f.ip = ?"
	args := []interface{}{ip}

	countQuery := `SELECT COUNT(DISTINCT f.user_id) FROM user_fingerprints f WHERE ` + baseWhere
	err := DB.Raw(countQuery, args...).Scan(&total).Error
	if err != nil {
		return nil, 0, err
	}

	query := `
		SELECT u.id, u.username, u.display_name, u.email, u.status, u.role,
			   u.quota, u.used_quota, u.request_count,
			   f.visitor_id, f.created_at as record_time, f.ip
		FROM user_fingerprints f
		JOIN users u ON f.user_id = u.id
		WHERE ` + baseWhere + `
		AND f.id IN (
			SELECT MAX(f2.id) FROM user_fingerprints f2
			WHERE f2.ip = ?
			GROUP BY f2.user_id
		)
		ORDER BY f.created_at DESC
		LIMIT ? OFFSET ?
	`

	queryArgs := []interface{}{ip, ip, pageInfo.GetPageSize(), pageInfo.GetStartIdx()}
	err = DB.Raw(query, queryArgs...).Scan(&results).Error
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

// GetDuplicateVisitorIds 获取有多个用户使用的visitor id列表（需要visitor_id和ip都相同才算重复）
func GetDuplicateVisitorIds(pageInfo *common.PageInfo) ([]map[string]interface{}, int64, error) {
	var total int64

	// 统计有重复的visitor_id + ip组合数量
	countQuery := `
		SELECT COUNT(*) FROM (
			SELECT visitor_id, ip FROM user_fingerprints 
			GROUP BY visitor_id, ip 
			HAVING COUNT(DISTINCT user_id) > 1
		) AS duplicates
	`
	err := DB.Raw(countQuery).Scan(&total).Error
	if err != nil {
		return nil, 0, err
	}

	// 查询重复的visitor_id + ip组合及其用户数
	query := `
		SELECT visitor_id, ip, COUNT(DISTINCT user_id) as user_count, MAX(created_at) as last_seen
		FROM user_fingerprints
		GROUP BY visitor_id, ip
		HAVING COUNT(DISTINCT user_id) > 1
		ORDER BY user_count DESC, last_seen DESC
		LIMIT ? OFFSET ?
	`

	var results []struct {
		VisitorId string    `json:"visitor_id"`
		IP        string    `json:"ip"`
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
			"ip":         r.IP,
			"user_count": r.UserCount,
			"last_seen":  r.LastSeen,
		})
	}

	return mapResults, total, nil
}
