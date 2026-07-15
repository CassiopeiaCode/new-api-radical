package model

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	MaxFingerprintsPerUser  = 5
	maxFingerprintVisitorID = 128
	maxFingerprintUserAgent = 512
)

// UserFingerprint is a privacy-sensitive, administrator-only risk and
// diagnostics association. A user may have at most five recent distinct
// (visitor_id, ip) pairs; the database constraint is the authoritative
// deduplication guard under concurrent reports.
type UserFingerprint struct {
	Id int `json:"id" gorm:"primaryKey;autoIncrement"`

	UserId    int       `json:"user_id" gorm:"not null;index;uniqueIndex:ux_user_fingerprints_user_visitor_ip"`
	VisitorId string    `json:"visitor_id" gorm:"type:varchar(128);not null;index;uniqueIndex:ux_user_fingerprints_user_visitor_ip"`
	IP        string    `json:"ip" gorm:"type:varchar(64);not null;index;uniqueIndex:ux_user_fingerprints_user_visitor_ip"`
	UserAgent string    `json:"user_agent" gorm:"type:varchar(512)"`
	CreatedAt time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt time.Time `json:"updated_at" gorm:"autoUpdateTime"`
}

func (UserFingerprint) TableName() string { return "user_fingerprints" }

func ValidateFingerprintInput(visitorID, ip, userAgent string) (string, string, string, error) {
	visitorID = strings.TrimSpace(visitorID)
	if visitorID == "" || len(visitorID) > maxFingerprintVisitorID {
		return "", "", "", fmt.Errorf("visitor_id must be between 1 and %d bytes", maxFingerprintVisitorID)
	}
	for _, r := range visitorID {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return "", "", "", errors.New("visitor_id contains unsupported characters")
	}

	ip = strings.TrimSpace(ip)
	if parsed := net.ParseIP(ip); parsed == nil {
		return "", "", "", errors.New("invalid client ip")
	}
	if len(userAgent) > maxFingerprintUserAgent {
		userAgent = userAgent[:maxFingerprintUserAgent]
	}
	return visitorID, ip, userAgent, nil
}

// RecordFingerprint upserts the reported pair and then keeps exactly the
// newest MaxFingerprintsPerUser records. Locking the owning user row makes
// the upsert-and-trim operation serial for one user on databases that support
// row locking; the unique index remains the final concurrency backstop.
func RecordFingerprint(userID int, visitorID, userAgent, ip string) error {
	if userID <= 0 {
		return errors.New("invalid user id")
	}
	visitorID, ip, userAgent, err := ValidateFingerprintInput(visitorID, ip, userAgent)
	if err != nil {
		return err
	}

	return DB.Transaction(func(tx *gorm.DB) error {
		var user User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").First(&user, userID).Error; err != nil {
			return err
		}

		now := time.Now()
		fingerprint := UserFingerprint{
			UserId: userID, VisitorId: visitorID, IP: ip, UserAgent: userAgent, UpdatedAt: now,
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "user_id"}, {Name: "visitor_id"}, {Name: "ip"}},
			DoUpdates: clause.AssignmentColumns([]string{"user_agent", "updated_at"}),
		}).Create(&fingerprint).Error; err != nil {
			return err
		}

		var boundary UserFingerprint
		err := tx.Select("id", "updated_at").Where("user_id = ?", userID).
			Order("updated_at DESC, id DESC").Offset(MaxFingerprintsPerUser - 1).Limit(1).Take(&boundary).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		return tx.Where("user_id = ?", userID).
			Where("updated_at < ? OR (updated_at = ? AND id < ?)", boundary.UpdatedAt, boundary.UpdatedAt, boundary.Id).
			Delete(&UserFingerprint{}).Error
	})
}

func GetUserFingerprints(userID int) ([]UserFingerprint, error) {
	var fingerprints []UserFingerprint
	err := DB.Where("user_id = ?", userID).Order("updated_at DESC, id DESC").Limit(MaxFingerprintsPerUser).Find(&fingerprints).Error
	return fingerprints, err
}

type UserWithFingerprint struct {
	Id          int       `json:"id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
	Email       string    `json:"email"`
	Status      int       `json:"status"`
	Role        int       `json:"role"`
	VisitorId   string    `json:"visitor_id"`
	IP          string    `json:"ip"`
	RecordTime  time.Time `json:"record_time"`
}

type DuplicateFingerprint struct {
	VisitorId string `json:"visitor_id"`
	IP        string `json:"ip"`
	UserCount int64  `json:"user_count"`
	LastSeen  int64  `json:"last_seen"`
}

func fingerprintUserSelect() string {
	return `SELECT u.id, u.username, u.display_name, u.email, u.status, u.role,
        f.visitor_id, f.ip, f.updated_at AS record_time
        FROM user_fingerprints f JOIN users u ON u.id = f.user_id`
}

func GetAllFingerprints(pageInfo *common.PageInfo, keyword string) ([]UserWithFingerprint, int64, error) {
	keyword = strings.TrimSpace(keyword)
	where := ""
	args := []any{}
	if keyword != "" {
		where = " WHERE f.visitor_id LIKE ? OR f.ip LIKE ? OR u.username LIKE ? OR u.email LIKE ?"
		like := "%" + keyword + "%"
		args = append(args, like, like, like, like)
	}
	var total int64
	if err := DB.Raw("SELECT COUNT(*) FROM user_fingerprints f JOIN users u ON u.id = f.user_id"+where, args...).Scan(&total).Error; err != nil {
		return nil, 0, err
	}
	queryArgs := append(append([]any{}, args...), pageInfo.GetPageSize(), pageInfo.GetStartIdx())
	var results []UserWithFingerprint
	err := DB.Raw(fingerprintUserSelect()+where+" ORDER BY f.updated_at DESC, f.id DESC LIMIT ? OFFSET ?", queryArgs...).Scan(&results).Error
	return results, total, err
}

func FindUsersByFingerprint(visitorID, ip string, pageInfo *common.PageInfo) ([]UserWithFingerprint, int64, error) {
	visitorID = strings.TrimSpace(visitorID)
	ip = strings.TrimSpace(ip)
	if visitorID == "" && ip == "" {
		return nil, 0, errors.New("visitor_id or ip is required")
	}
	clauses := make([]string, 0, 2)
	args := make([]any, 0, 2)
	if visitorID != "" {
		clauses, args = append(clauses, "f.visitor_id = ?"), append(args, visitorID)
	}
	if ip != "" {
		clauses, args = append(clauses, "f.ip = ?"), append(args, ip)
	}
	where := " WHERE " + strings.Join(clauses, " AND ")
	var total int64
	if err := DB.Raw("SELECT COUNT(DISTINCT f.user_id) FROM user_fingerprints f"+where, args...).Scan(&total).Error; err != nil {
		return nil, 0, err
	}
	queryArgs := append(append([]any{}, args...), pageInfo.GetPageSize(), pageInfo.GetStartIdx())
	var results []UserWithFingerprint
	err := DB.Raw(fingerprintUserSelect()+where+" ORDER BY f.updated_at DESC, f.id DESC LIMIT ? OFFSET ?", queryArgs...).Scan(&results).Error
	return results, total, err
}

func GetDuplicateFingerprints(pageInfo *common.PageInfo) ([]DuplicateFingerprint, int64, error) {
	base := " FROM user_fingerprints GROUP BY visitor_id, ip HAVING COUNT(DISTINCT user_id) > 1"
	var total int64
	if err := DB.Raw("SELECT COUNT(*) FROM (SELECT visitor_id, ip" + base + ") duplicate_fingerprints").Scan(&total).Error; err != nil {
		return nil, 0, err
	}
	var results []DuplicateFingerprint
	lastSeen := "UNIX_TIMESTAMP(MAX(updated_at))"
	if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		lastSeen = "CAST(strftime('%s', MAX(updated_at)) AS INTEGER)"
	} else if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		lastSeen = "CAST(EXTRACT(EPOCH FROM MAX(updated_at)) AS BIGINT)"
	}
	err := DB.Raw("SELECT visitor_id, ip, COUNT(DISTINCT user_id) AS user_count, "+lastSeen+" AS last_seen"+base+" ORDER BY user_count DESC, last_seen DESC LIMIT ? OFFSET ?", pageInfo.GetPageSize(), pageInfo.GetStartIdx()).Scan(&results).Error
	return results, total, err
}
