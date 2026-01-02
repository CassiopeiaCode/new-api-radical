package operation_setting

import "github.com/QuantumNous/new-api/setting/config"

// CheckinSetting 签到功能配置
// 兼容上游字段命名，同时保留固定/随机模式功能
type CheckinSetting struct {
	Enabled    bool `json:"enabled"`     // 是否启用签到功能（上游字段）
	MinQuota   int  `json:"min_quota"`   // 签到最小额度（上游字段，随机模式下使用）
	MaxQuota   int  `json:"max_quota"`   // 签到最大额度（上游字段，随机模式下使用）
	FixedQuota int  `json:"fixed_quota"` // 固定签到额度（扩展字段，固定模式下使用）
	RandomMode bool `json:"random_mode"` // 是否启用随机额度模式（扩展字段，默认true与上游行为一致）
}

// 默认配置
var checkinSetting = CheckinSetting{
	Enabled:    false,
	MinQuota:   1000,
	MaxQuota:   10000,
	FixedQuota: 5000,
	RandomMode: true, // 默认随机模式，与上游行为一致
}

func init() {
	config.GlobalConfig.Register("checkin_setting", &checkinSetting)
}

func GetCheckinSetting() *CheckinSetting {
	return &checkinSetting
}

// IsCheckinEnabled 是否启用签到功能
func IsCheckinEnabled() bool {
	return checkinSetting.Enabled
}

// GetCheckinQuotaRange 获取签到额度范围（兼容上游）
func GetCheckinQuotaRange() (min, max int) {
	return checkinSetting.MinQuota, checkinSetting.MaxQuota
}
