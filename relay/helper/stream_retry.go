package helper

import (
	"fmt"
	"net/http"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func StreamAttemptError(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	if info == nil || !info.IsStream || info.StreamStatus == nil {
		return nil
	}
	if HasSemanticOutput(c) {
		return nil
	}
	if info.StreamStatus.EndReason == relaycommon.StreamEndReasonDone ||
		(info.StreamStatus.EndReason == relaycommon.StreamEndReasonHandlerStop && info.StreamStatus.EndError == nil) {
		return nil
	}
	message := fmt.Sprintf("upstream stream ended before semantic output: %s", info.StreamStatus.Summary())
	return types.NewOpenAIError(fmt.Errorf("%s", message), types.ErrorCodeBadResponse, http.StatusBadGateway)
}
