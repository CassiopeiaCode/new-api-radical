package operation_setting

import "github.com/QuantumNous/new-api/setting/config"

type CheckinSetting struct {
	CheckinEnabled    bool `json:"checkin_enabled"`     // 是否启用签到功能
	CheckinQuota      int  `json:"checkin_quota"`       // 签到奖励额度（固定模式）
	CheckinMinQuota   int  `json:"checkin_min_quota"`   // 签到最小额度（随机模式）
	CheckinMaxQuota   int  `json:"checkin_max_quota"`   // 签到最大额度（随机模式）
	CheckinRandomMode bool `json:"checkin_random_mode"` // 是否启用随机额度模式
}

// 默认配置
var checkinSetting = CheckinSetting{
	CheckinEnabled:    false,
	CheckinQuota:      1000,
	CheckinMinQuota:   500,
	CheckinMaxQuota:   2000,
	CheckinRandomMode: false,
}

func init() {
	// 注册到全局配置管理器
	config.GlobalConfig.Register("checkin_setting", &checkinSetting)
}

func GetCheckinSetting() *CheckinSetting {
	return &checkinSetting
}
