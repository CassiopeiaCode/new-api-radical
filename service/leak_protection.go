package service

import (
	"errors"
	"math"
	"net/url"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

const leakProtectionScanLimit = 3

var (
	leakCandidatePattern    = regexp.MustCompile(`[A-Za-z0-9][A-Za-z0-9+/_=.\-]{7,}`)
	leakUUIDPattern         = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	leakDatePattern         = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	leakTimePattern         = regexp.MustCompile(`^\d{1,2}:\d{2}(:\d{2})?$`)
	leakVersionPattern      = regexp.MustCompile(`(?i)^v?\d+(\.\d+){1,3}([\-_.]?[a-z0-9]+)?$`)
	leakCredentialHintWords = []string{
		"api_key", "apikey", "token", "secret", "password", "passwd", "authorization",
		"auth", "bearer", "cookie", "session", "credential", "private_key", "access_key",
	}
)

type leakTextFragment struct {
	Text string
}

func IsLeakProtectionStrictEnabled(setting dto.UserSetting) bool {
	return !setting.DisableLeakProtectionStrict
}

func CheckRequestLeakProtection(request dto.Request) (bool, string) {
	fragments := extractLeakProtectionTexts(request)
	for _, fragment := range fragments {
		if textContainsLeakProtectionSecret(fragment.Text) {
			return true, "request contains high-entropy credential-like content"
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

func textContainsLeakProtectionSecret(text string) bool {
	for _, raw := range leakCandidatePattern.FindAllString(text, -1) {
		candidate := strings.Trim(raw, "\"'")
		candidate = strings.Trim(candidate, "[](){}<>,;")
		if isLeakProtectionSafeCandidate(candidate) {
			continue
		}
		if isLeakProtectionUUIDComposite(candidate) {
			return true
		}
		if isHighEntropyLeakProtectionCandidate(candidate, hasLeakProtectionCredentialHint(text, candidate)) {
			return true
		}
	}
	return false
}

func isLeakProtectionSafeCandidate(candidate string) bool {
	if candidate == "" {
		return true
	}
	lower := strings.ToLower(candidate)
	if lower == "uuid" {
		return true
	}
	if isLeakProtectionNumericOnly(candidate) {
		return true
	}
	if leakUUIDPattern.MatchString(candidate) {
		return true
	}
	if leakDatePattern.MatchString(candidate) || leakTimePattern.MatchString(candidate) || leakVersionPattern.MatchString(candidate) {
		return true
	}
	if isLeakProtectionURL(candidate) {
		return true
	}
	if isLeakProtectionNaturalWord(candidate) {
		return true
	}
	return false
}

func isLeakProtectionUUIDComposite(candidate string) bool {
	lower := strings.ToLower(candidate)
	if lower == "uuid" {
		return false
	}
	return strings.Contains(lower, "uuid") && (strings.Contains(candidate, "-") || strings.Contains(candidate, "_") || strings.Contains(candidate, ":"))
}

func hasLeakProtectionCredentialHint(text string, candidate string) bool {
	lowerText := strings.ToLower(text)
	lowerCandidate := strings.ToLower(candidate)
	idx := strings.Index(lowerText, lowerCandidate)
	if idx < 0 {
		return false
	}
	start := idx - 48
	if start < 0 {
		start = 0
	}
	end := idx + len(lowerCandidate) + 48
	if end > len(lowerText) {
		end = len(lowerText)
	}
	window := lowerText[start:end]
	for _, hint := range leakCredentialHintWords {
		if strings.Contains(window, hint) {
			return true
		}
	}
	return false
}

func isHighEntropyLeakProtectionCandidate(candidate string, hasHint bool) bool {
	if len(candidate) < 8 || isLeakProtectionNumericOnly(candidate) {
		return false
	}
	classCount := leakProtectionClassCount(candidate)
	entropy := leakProtectionEntropy(candidate)

	switch {
	case len(candidate) >= 24:
		return classCount >= 2 && entropy >= 3.0
	case len(candidate) >= 16:
		threshold := 3.3
		if hasHint {
			threshold = 3.0
		}
		return classCount >= 2 && entropy >= threshold
	case len(candidate) >= 12:
		threshold := 3.5
		if hasHint {
			threshold = 3.1
		}
		return classCount >= 3 && entropy >= threshold
	default:
		threshold := 3.8
		if hasHint {
			threshold = 3.35
		}
		return classCount >= 3 && entropy >= threshold
	}
}

func isLeakProtectionNumericOnly(candidate string) bool {
	for _, r := range candidate {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func isLeakProtectionURL(candidate string) bool {
	if !strings.Contains(candidate, "://") {
		return false
	}
	parsed, err := url.Parse(candidate)
	return err == nil && parsed.Scheme != "" && parsed.Host != ""
}

func isLeakProtectionNaturalWord(candidate string) bool {
	hasLetter := false
	for _, r := range candidate {
		switch {
		case r >= 'a' && r <= 'z':
			hasLetter = true
		case r >= 'A' && r <= 'Z':
			hasLetter = true
		default:
			return false
		}
	}
	return hasLetter
}

func leakProtectionClassCount(candidate string) int {
	var hasLower, hasUpper, hasDigit, hasSpecial bool
	for _, r := range candidate {
		switch {
		case r >= 'a' && r <= 'z':
			hasLower = true
		case r >= 'A' && r <= 'Z':
			hasUpper = true
		case r >= '0' && r <= '9':
			hasDigit = true
		default:
			hasSpecial = true
		}
	}
	count := 0
	if hasLower {
		count++
	}
	if hasUpper {
		count++
	}
	if hasDigit {
		count++
	}
	if hasSpecial {
		count++
	}
	return count
}

func leakProtectionEntropy(candidate string) float64 {
	if candidate == "" {
		return 0
	}
	freq := make(map[rune]int)
	for _, r := range candidate {
		freq[r]++
	}
	length := float64(len([]rune(candidate)))
	entropy := 0.0
	for _, count := range freq {
		p := float64(count) / length
		entropy -= p * math.Log2(p)
	}
	return entropy
}

func NewLeakProtectionBlockedError() error {
	return errors.New("request contains suspected sensitive credentials and was blocked by leak protection")
}
