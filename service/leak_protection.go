package service

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/types"
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
	return common.LeakProtectionBalancedForceEnabled || !setting.DisableLeakProtectionBalanced
}

// CheckRequestLeakProtection examines normalized request text only. It never
// returns matching content, so audit paths cannot reproduce a credential.
func CheckRequestLeakProtection(request dto.Request) (bool, string) {
	if request == nil {
		return false, ""
	}
	meta := leakProtectionTokenCountMeta(request)
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

// leakProtectionTokenCountMeta limits conversational scanning to the suffix
// beginning with the second-most-recent user message. Clients resend the full
// conversation on every turn; rescanning older history is both redundant and
// disproportionately expensive for long-running conversations.
func leakProtectionTokenCountMeta(request dto.Request) *types.TokenCountMeta {
	switch r := request.(type) {
	case *dto.GeneralOpenAIRequest:
		clone := *r
		if start, ok := secondLastUserIndex(len(r.Messages), func(i int) string { return r.Messages[i].Role }); ok {
			clone.Messages = r.Messages[start:]
			clone.Prompt, clone.Input, clone.Tools = nil, nil, nil
		}
		return clone.GetTokenCountMeta()
	case *dto.ClaudeRequest:
		clone := *r
		if start, ok := secondLastUserIndex(len(r.Messages), func(i int) string { return r.Messages[i].Role }); ok {
			clone.Messages = r.Messages[start:]
			clone.System, clone.Tools = nil, nil
		}
		return clone.GetTokenCountMeta()
	case *dto.GeminiChatRequest:
		clone := *r
		if start, ok := secondLastUserIndex(len(r.Contents), func(i int) string { return r.Contents[i].Role }); ok {
			clone.Contents = r.Contents[start:]
			clone.SystemInstructions, clone.Tools, clone.ToolConfig = nil, nil, nil
		}
		return clone.GetTokenCountMeta()
	case *dto.OpenAIResponsesRequest:
		clone := *r
		var items []struct {
			Role string `json:"role"`
		}
		if common.GetJsonType(r.Input) == "array" && common.Unmarshal(r.Input, &items) == nil {
			if start, ok := secondLastUserIndex(len(items), func(i int) string { return items[i].Role }); ok {
				var rawItems []json.RawMessage
				if common.Unmarshal(r.Input, &rawItems) == nil {
					clone.Input, _ = common.Marshal(rawItems[start:])
				}
				clone.Instructions, clone.Metadata, clone.Text = nil, nil, nil
				clone.ToolChoice, clone.Tools, clone.Prompt = nil, nil, nil
			}
		}
		return clone.GetTokenCountMeta()
	default:
		return request.GetTokenCountMeta()
	}
}

func secondLastUserIndex(length int, roleAt func(int) string) (int, bool) {
	usersSeen := 0
	for i := length - 1; i >= 0; i-- {
		if strings.EqualFold(roleAt(i), "user") {
			usersSeen++
			if usersSeen == 2 {
				return i, true
			}
		}
	}
	for i := 0; i < length; i++ {
		if strings.EqualFold(roleAt(i), "user") {
			return i, true
		}
	}
	return 0, false
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
