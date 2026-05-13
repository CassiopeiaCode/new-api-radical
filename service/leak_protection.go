package service

import (
	"errors"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	gitleaksconfig "github.com/zricethezav/gitleaks/v8/config"
	gitleaksdetect "github.com/zricethezav/gitleaks/v8/detect"
)

const leakProtectionScanLimit = 3

var (
	leakProtectionGitleaksConfigOnce sync.Once
	leakProtectionGitleaksConfig     gitleaksconfig.Config
	leakProtectionGitleaksConfigErr  error
)

type leakTextFragment struct {
	Text string
}

func IsLeakProtectionBalancedEnabled(setting dto.UserSetting) bool {
	return !setting.DisableLeakProtectionBalanced
}

func CheckRequestLeakProtection(request dto.Request) (bool, string) {
	fragments := extractLeakProtectionTexts(request)
	for _, fragment := range fragments {
		if blocked, reason := textContainsLeakProtectionSecret(fragment.Text); blocked {
			return true, reason
		}
	}
	return false, ""
}

func extractLeakProtectionTexts(request dto.Request) []leakTextFragment {
	switch r := request.(type) {
	case *dto.GeneralOpenAIRequest:
		return extractOpenAIMessageTexts(r.Messages)
	case *dto.OpenAIResponsesRequest:
		return extractResponsesTexts(r)
	case *dto.ClaudeRequest:
		return extractClaudeTexts(r)
	default:
		return nil
	}
}

func extractOpenAIMessageTexts(messages []dto.Message) []leakTextFragment {
	fragments := make([]leakTextFragment, 0, leakProtectionScanLimit)
	for i := len(messages) - 1; i >= 0 && len(fragments) < leakProtectionScanLimit; i-- {
		role := strings.ToLower(messages[i].Role)
		if role != "user" && role != "tool" {
			continue
		}
		text := extractOpenAIMessageText(messages[i])
		if text == "" {
			continue
		}
		fragments = append(fragments, leakTextFragment{Text: text})
	}
	return fragments
}

func extractOpenAIMessageText(message dto.Message) string {
	var texts []string
	if content := strings.TrimSpace(message.StringContent()); content != "" {
		texts = append(texts, content)
	} else {
		for _, part := range message.ParseContent() {
			if part.Type == dto.ContentTypeText && strings.TrimSpace(part.Text) != "" {
				texts = append(texts, part.Text)
			}
		}
	}
	if len(texts) == 0 {
		if fallback := serializeLeakProtectionValue(message.Content); fallback != "" {
			texts = append(texts, fallback)
		}
	}
	return strings.Join(texts, "\n")
}

func extractResponsesTexts(request *dto.OpenAIResponsesRequest) []leakTextFragment {
	fragments := make([]leakTextFragment, 0, leakProtectionScanLimit)
	if request == nil || len(request.Input) == 0 {
		return fragments
	}

	if common.GetJsonType(request.Input) == "string" {
		var text string
		_ = common.Unmarshal(request.Input, &text)
		if strings.TrimSpace(text) != "" {
			fragments = append(fragments, leakTextFragment{Text: text})
		}
		return fragments
	}

	var inputs []dto.Input
	if err := common.Unmarshal(request.Input, &inputs); err != nil {
		return fragments
	}
	for i := len(inputs) - 1; i >= 0 && len(fragments) < leakProtectionScanLimit; i-- {
		role := strings.ToLower(inputs[i].Role)
		if role != "" && role != "user" && role != "tool" {
			continue
		}
		text := extractResponsesInputText(inputs[i])
		if text == "" {
			continue
		}
		fragments = append(fragments, leakTextFragment{Text: text})
	}
	return fragments
}

func extractResponsesInputText(input dto.Input) string {
	switch common.GetJsonType(input.Content) {
	case "string":
		var text string
		_ = common.Unmarshal(input.Content, &text)
		return strings.TrimSpace(text)
	case "array":
		var items []map[string]any
		if err := common.Unmarshal(input.Content, &items); err != nil {
			return ""
		}
		texts := make([]string, 0, len(items))
		for _, item := range items {
			if text, _ := item["text"].(string); strings.TrimSpace(text) != "" {
				texts = append(texts, text)
				continue
			}
			if serialized := serializeLeakProtectionValue(item); serialized != "" {
				texts = append(texts, serialized)
			}
		}
		return strings.Join(texts, "\n")
	default:
		return strings.TrimSpace(string(input.Content))
	}
}

func extractClaudeTexts(request *dto.ClaudeRequest) []leakTextFragment {
	fragments := make([]leakTextFragment, 0, leakProtectionScanLimit)
	if request == nil {
		return fragments
	}
	for i := len(request.Messages) - 1; i >= 0 && len(fragments) < leakProtectionScanLimit; i-- {
		role := strings.ToLower(request.Messages[i].Role)
		if role != "user" && role != "tool" {
			continue
		}
		text := extractClaudeMessageText(request.Messages[i])
		if text == "" {
			continue
		}
		fragments = append(fragments, leakTextFragment{Text: text})
	}
	return fragments
}

func extractClaudeMessageText(message dto.ClaudeMessage) string {
	var texts []string
	if content := strings.TrimSpace(message.GetStringContent()); content != "" {
		texts = append(texts, content)
	}
	mediaItems, err := message.ParseContent()
	if err == nil {
		for _, item := range mediaItems {
			if text := strings.TrimSpace(item.GetText()); text != "" {
				texts = append(texts, text)
			}
			if text := strings.TrimSpace(item.GetStringContent()); text != "" {
				texts = append(texts, text)
			}
			if serialized := serializeLeakProtectionValue(item.Content); serialized != "" {
				texts = append(texts, serialized)
			}
			if serialized := serializeLeakProtectionValue(item.Input); serialized != "" {
				texts = append(texts, serialized)
			}
		}
	}
	if len(texts) == 0 {
		if serialized := serializeLeakProtectionValue(message.Content); serialized != "" {
			texts = append(texts, serialized)
		}
	}
	return strings.Join(dedupLeakProtectionTexts(texts), "\n")
}

func dedupLeakProtectionTexts(texts []string) []string {
	seen := make(map[string]struct{}, len(texts))
	result := make([]string, 0, len(texts))
	for _, text := range texts {
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		if _, ok := seen[text]; ok {
			continue
		}
		seen[text] = struct{}{}
		result = append(result, text)
	}
	return result
}

func serializeLeakProtectionValue(value any) string {
	if value == nil {
		return ""
	}
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case []byte:
		return strings.TrimSpace(string(v))
	}
	data, err := common.Marshal(value)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func textContainsLeakProtectionSecret(text string) (bool, string) {
	cfg, err := getLeakProtectionGitleaksConfig()
	if err != nil {
		common.SysError("failed to initialize gitleaks config for leak protection: " + err.Error())
		return false, ""
	}
	detector := gitleaksdetect.NewDetector(cfg)
	findings := detector.DetectString(text)
	if len(findings) == 0 {
		return false, ""
	}
	finding := findings[0]
	if finding.RuleID != "" {
		return true, "request matched gitleaks rule " + finding.RuleID
	}
	if finding.Description != "" {
		return true, "request matched gitleaks rule: " + finding.Description
	}
	return true, "request matched gitleaks default config"
}

func getLeakProtectionGitleaksConfig() (gitleaksconfig.Config, error) {
	leakProtectionGitleaksConfigOnce.Do(func() {
		detector, err := gitleaksdetect.NewDetectorDefaultConfig()
		if err != nil {
			leakProtectionGitleaksConfigErr = err
			return
		}
		leakProtectionGitleaksConfig = detector.Config
	})
	return leakProtectionGitleaksConfig, leakProtectionGitleaksConfigErr
}

func NewLeakProtectionBlockedError() error {
	return errors.New("request contains suspected sensitive credentials and was blocked by leak protection; you can disable this protection in Personal Settings if needed")
}
