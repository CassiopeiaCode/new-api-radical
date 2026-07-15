package setting

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/QuantumNous/new-api/common"
)

var ModelRequestRateLimitEnabled = false
var ModelRequestRateLimitDurationMinutes = 1
var ModelRequestRateLimitCount = 0
var ModelRequestRateLimitSuccessCount = 1000
var ModelRequestRateLimitGroup = map[string][2]int{}
var ModelRequestRateLimitExemptUserIDs = map[int]struct{}{}
var ModelRequestRateLimitMutex sync.RWMutex

func ModelRequestRateLimitGroup2JSONString() string {
	ModelRequestRateLimitMutex.RLock()
	defer ModelRequestRateLimitMutex.RUnlock()

	jsonBytes, err := json.Marshal(ModelRequestRateLimitGroup)
	if err != nil {
		common.SysLog("error marshalling model ratio: " + err.Error())
	}
	return string(jsonBytes)
}

func UpdateModelRequestRateLimitGroupByJSONString(jsonStr string) error {
	ModelRequestRateLimitMutex.Lock()
	defer ModelRequestRateLimitMutex.Unlock()

	groups := make(map[string][2]int)
	if err := json.Unmarshal([]byte(jsonStr), &groups); err != nil {
		return err
	}
	ModelRequestRateLimitGroup = groups
	return nil
}

// ParseModelRequestRateLimitExemptUserIDs accepts comma or whitespace separated
// positive user IDs. Keeping the persisted setting textual makes it easy to edit
// from either administration UI while the runtime lookup remains O(1).
func ParseModelRequestRateLimitExemptUserIDs(raw string) (map[int]struct{}, error) {
	ids := make(map[int]struct{})
	for _, token := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || unicode.IsSpace(r)
	}) {
		id, err := strconv.Atoi(strings.TrimSpace(token))
		if err != nil {
			return nil, fmt.Errorf("invalid user ID %q", token)
		}
		if id > 0 {
			ids[id] = struct{}{}
		}
	}
	return ids, nil
}

func UpdateModelRequestRateLimitExemptUserIDs(raw string) error {
	ids, err := ParseModelRequestRateLimitExemptUserIDs(raw)
	if err != nil {
		return err
	}
	ModelRequestRateLimitMutex.Lock()
	defer ModelRequestRateLimitMutex.Unlock()
	ModelRequestRateLimitExemptUserIDs = ids
	return nil
}

func IsModelRequestRateLimitExemptUser(userID int) bool {
	if userID <= 0 {
		return false
	}
	ModelRequestRateLimitMutex.RLock()
	defer ModelRequestRateLimitMutex.RUnlock()
	_, ok := ModelRequestRateLimitExemptUserIDs[userID]
	return ok
}

func GetGroupRateLimit(group string) (totalCount, successCount int, found bool) {
	ModelRequestRateLimitMutex.RLock()
	defer ModelRequestRateLimitMutex.RUnlock()

	if ModelRequestRateLimitGroup == nil {
		return 0, 0, false
	}

	limits, found := ModelRequestRateLimitGroup[group]
	if !found {
		return 0, 0, false
	}
	return limits[0], limits[1], true
}

func CheckModelRequestRateLimitGroup(jsonStr string) error {
	checkModelRequestRateLimitGroup := make(map[string][2]int)
	err := json.Unmarshal([]byte(jsonStr), &checkModelRequestRateLimitGroup)
	if err != nil {
		return err
	}
	for group, limits := range checkModelRequestRateLimitGroup {
		if limits[0] < 0 || limits[1] < 1 {
			return fmt.Errorf("group %s has negative rate limit values: [%d, %d]", group, limits[0], limits[1])
		}
		if limits[0] > math.MaxInt32 || limits[1] > math.MaxInt32 {
			return fmt.Errorf("group %s [%d, %d] has max rate limits value 2147483647", group, limits[0], limits[1])
		}
	}

	return nil
}
