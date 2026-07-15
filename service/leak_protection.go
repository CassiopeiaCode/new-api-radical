package service

import (
	"errors"
	"regexp"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/dto"
	gitleaksconfig "github.com/zricethezav/gitleaks/v8/config"
	gitleaksdetect "github.com/zricethezav/gitleaks/v8/detect"
)

var (
	leakProtectionConfigOnce sync.Once
	leakProtectionConfig     gitleaksconfig.Config
	leakProtectionConfigErr  error
	leakProtectionPool       sync.Pool
	leakProtectionSK         = regexp.MustCompile(`\bsk-[A-Za-z0-9]{40,}\b`)
)

func IsLeakProtectionBalancedEnabled(setting dto.UserSetting) bool {
	return !setting.DisableLeakProtectionBalanced
}

// CheckRequestLeakProtection examines normalized request text only. It never
// returns matching content, so audit paths cannot reproduce a credential.
func CheckRequestLeakProtection(request dto.Request) (bool, string) {
	if request == nil {
		return false, ""
	}
	meta := request.GetTokenCountMeta()
	if meta == nil || strings.TrimSpace(meta.CombineText) == "" {
		return false, ""
	}
	text := meta.CombineText
	if leakProtectionSK.MatchString(text) {
		return true, "matched custom sk token fallback rule"
	}
	detector, err := getLeakProtectionDetector()
	if err != nil {
		return true, "leak protection scanner unavailable"
	}
	defer leakProtectionPool.Put(detector)
	findings := detector.DetectString(text)
	if len(findings) == 0 {
		return false, ""
	}
	if findings[0].RuleID != "" {
		return true, "matched gitleaks rule " + findings[0].RuleID
	}
	return true, "matched gitleaks rule"
}

func getLeakProtectionDetector() (*gitleaksdetect.Detector, error) {
	if detector, ok := leakProtectionPool.Get().(*gitleaksdetect.Detector); ok && detector != nil {
		return detector, nil
	}
	leakProtectionConfigOnce.Do(func() {
		detector, err := gitleaksdetect.NewDetectorDefaultConfig()
		if err != nil {
			leakProtectionConfigErr = err
			return
		}
		leakProtectionConfig = detector.Config
	})
	if leakProtectionConfigErr != nil {
		return nil, leakProtectionConfigErr
	}
	return gitleaksdetect.NewDetector(leakProtectionConfig), nil
}

func NewLeakProtectionBlockedError() error { return errors.New("request blocked by leak protection") }
