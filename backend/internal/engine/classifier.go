package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"oci-panel/internal/oci"

	"github.com/oracle/oci-go-sdk/v65/common"
)

type ErrorCategory int

const (
	CategoryCapacityFull ErrorCategory = iota // Out of host capacity -> keep retrying
	CategoryRateLimited                       // 429 Too Many Requests -> exponential backoff
	CategoryTransient                         // 5xx / 409 / network trouble -> retry after the normal interval
	CategoryFatalError                        // Invalid params, limits exceeded, auth failure -> stop
	CategoryCancelled                         // Worker context cancelled (task stopped) -> exit quietly
)

// ClassifyError decides retry vs. stop from the OCI service error (status + code), never from
// substrings of the rendered error text (which embeds a hex opc-request-id and can contain "500").
//
// OCI semantics (docs.oracle.com/.../apierrors.htm):
//   - 500 InternalError "Out of host capacity."  -> no capacity in that AD right now
//   - 429 TooManyRequests                        -> back off
//   - 502 / 503 / 504, 409 IncorrectState        -> transient
//   - 400 LimitExceeded / InvalidParameter, 401, 403, 404 -> configuration or credential problem
func ClassifyError(err error) (ErrorCategory, string) {
	if err == nil {
		return CategoryCapacityFull, ""
	}
	if errors.Is(err, context.Canceled) {
		return CategoryCancelled, "任务已停止"
	}

	if se, ok := common.IsServiceError(err); ok {
		status := se.GetHTTPStatusCode()
		code := se.GetCode()
		msg := se.GetMessage()
		lowerMsg := strings.ToLower(msg)

		switch {
		case status == 429:
			return CategoryRateLimited, fmt.Sprintf("请求被限流 (429 %s)，启动指数退避", code)
		case status == 500 && (strings.Contains(lowerMsg, "out of host capacity") || strings.Contains(lowerMsg, "out of capacity")):
			return CategoryCapacityFull, "容量不足 (Out of host capacity)，等待下次轮询"
		case status == 500 || status == 502 || status == 503 || status == 504:
			return CategoryTransient, fmt.Sprintf("OCI 服务端临时错误 (%d %s)，稍后重试: %s", status, code, msg)
		case status == 409:
			return CategoryTransient, fmt.Sprintf("资源状态冲突 (409 %s)，稍后重试: %s", code, msg)
		case status == 401:
			return CategoryFatalError, fmt.Sprintf("认证失败 (401 %s)：API 密钥无效、已吊销，或服务器时间偏差过大", code)
		case status == 403 || status == 404:
			return CategoryFatalError, fmt.Sprintf("无权限或资源不存在 (%d %s)：请检查子网/镜像/区间是否正确且用户有权限: %s", status, code, msg)
		case status == 400 && strings.EqualFold(code, "LimitExceeded"):
			return CategoryFatalError, fmt.Sprintf("超出服务限额 (400 LimitExceeded)：%s", msg)
		case status == 400 && strings.EqualFold(code, "QuotaExceeded"):
			return CategoryFatalError, fmt.Sprintf("超出区间配额 (400 QuotaExceeded)：%s", msg)
		case status == 400:
			return CategoryFatalError, fmt.Sprintf("参数错误 (400 %s)：%s", code, msg)
		default:
			return CategoryFatalError, fmt.Sprintf("OCI 返回 %d %s：%s", status, code, msg)
		}
	}

	if errors.Is(err, context.DeadlineExceeded) || common.IsNetworkError(err) || oci.IsTransientError(err) {
		return CategoryTransient, "网络超时或连接失败，稍后重试: " + err.Error()
	}

	return CategoryFatalError, err.Error()
}
