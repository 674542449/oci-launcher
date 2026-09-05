package engine

import (
	"strings"

	"github.com/oracle/oci-go-sdk/v65/common"
)

type ErrorCategory int

const (
	CategoryCapacityFull ErrorCategory = iota // Out of host capacity -> retry
	CategoryRateLimited                       // 429 Too Many Requests -> exponential backoff
	CategoryFatalError                        // Invalid params, quota exceeded -> stop immediately
)

// ClassifyError categorizes OCI errors to decide retry vs fatal cutoff
func ClassifyError(err error) (ErrorCategory, string) {
	if err == nil {
		return CategoryCapacityFull, ""
	}

	errStr := strings.ToLower(err.Error())

	// 1. Capacity Full errors -> Continue retry
	if strings.Contains(errStr, "out of host capacity") ||
		strings.Contains(errStr, "outofhostcapacity") ||
		strings.Contains(errStr, "capacity") ||
		strings.Contains(errStr, "service unavailable") ||
		strings.Contains(errStr, "internal server error") ||
		strings.Contains(errStr, "500") ||
		strings.Contains(errStr, "502") ||
		strings.Contains(errStr, "503") ||
		strings.Contains(errStr, "504") {
		return CategoryCapacityFull, "容量不足 (Out of host capacity) 或云端临时网关拥堵，等待下次轮询"
	}

	// 2. 429 Rate limited -> Exponential backoff
	if serviceErr, ok := common.IsServiceError(err); ok && serviceErr.GetHTTPStatusCode() == 429 {
		return CategoryRateLimited, "遭遇 429 API 请求限流，启动指数退避阶梯"
	}
	if strings.Contains(errStr, "429") || strings.Contains(errStr, "toomanyrequests") {
		return CategoryRateLimited, "请求频率过高 (429 TooManyRequests)，启动指数退避"
	}

	// 3. Fatal errors -> STOP immediately, never retry bad config
	return CategoryFatalError, err.Error()
}
