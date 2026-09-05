package oci

import (
	"context"
	"fmt"
	"strings"
	"time"

	"oci-panel/internal/storage"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/core"
)

type AccountHealthResult struct {
	ProfileID uint   `json:"profile_id"`
	Status    string `json:"status"` // "Active", "Banned", "Invalid", "Error"
	IsHealthy bool   `json:"is_healthy"`
	Message   string `json:"message"`
	CheckedAt string `json:"checked_at"`
}

// CheckSingleAccountHealth probes one account with a single ListInstances call and classifies the
// outcome by the OCI service error code rather than by substrings of the error text.
// STRICT RULE: one account per call, never batch.
func CheckSingleAccountHealth(ctx context.Context, profile *storage.OCIProfile) (*AccountHealthResult, error) {
	result := &AccountHealthResult{
		ProfileID: profile.ID,
		CheckedAt: time.Now().Format(time.RFC3339),
	}

	computeClient, err := GetComputeClient(profile, profile.Region)
	if err != nil {
		result.Status = "Error"
		result.Message = fmt.Sprintf("凭据初始化失败: %v", err)
		return result, nil
	}

	resp, err := computeClient.ListInstances(ctx, core.ListInstancesRequest{
		CompartmentId: common.String(profile.TenancyOCID),
		Limit:         common.Int(1),
	})
	if err != nil {
		status, msg, pauseTasks := classifyHealthError(err)
		result.Status = status
		result.Message = msg

		storage.DB.Model(profile).Updates(map[string]interface{}{
			"status":         status,
			"status_message": msg,
		})

		// Only definitive credential/tenancy failures pause running tasks; permission or
		// transient errors must not stop a capacity hunt.
		if pauseTasks {
			storage.DB.Model(&storage.LaunchTask{}).
				Where("profile_id = ? AND status = ?", profile.ID, "running").
				Updates(map[string]interface{}{
					"status":       "stopped",
					"last_message": fmt.Sprintf("账号状态异常 (%s)，系统已自动熔断保护", status),
				})
		}
		return result, nil
	}

	msg := fmt.Sprintf("账号正常，API 连通（探测到实例数: %d）", len(resp.Items))
	storage.DB.Model(profile).Updates(map[string]interface{}{
		"status":         "Active",
		"status_message": msg,
	})

	// Email, registration time, tenancy name, country and subscription verdict for the account list
	EnrichProfileIdentity(ctx, profile)

	result.Status = "Active"
	result.IsHealthy = true
	result.Message = msg
	return result, nil
}

// classifyHealthError maps an SDK error to (profile status, message, pauseRunningTasks).
func classifyHealthError(err error) (string, string, bool) {
	httpStatus, code, isSvc := ServiceErrorInfo(err)
	lower := strings.ToLower(err.Error())
	suspended := strings.Contains(lower, "suspend") || strings.Contains(lower, "tenancydisabled") ||
		strings.Contains(lower, "userdisabled") || strings.Contains(lower, "has been deleted") ||
		strings.Contains(lower, "is disabled")

	switch {
	case suspended:
		return "Banned", "租户或用户已被停用/暂停（" + code + "），请登录 Oracle 控制台确认账号状态", true
	case isSvc && httpStatus == 401:
		return "Invalid", "API 凭据无效或已被吊销（401 " + code + "）。如果密钥确认无误，请检查服务器时间：与 Oracle 相差超过 5 分钟同样会返回 401", true
	case isSvc && (httpStatus == 403 || httpStatus == 404):
		return "Error", fmt.Sprintf("权限不足或资源不可见（%d %s）。请确认该用户对根区间有 inspect instances 权限", httpStatus, code), false
	case isSvc && httpStatus == 429:
		return "Error", "请求被 OCI 限流（429），请稍后重试", false
	case isSvc:
		return "Error", fmt.Sprintf("OCI API 返回 %d %s", httpStatus, code), false
	case IsTransientError(err):
		return "Error", "网络超时或连接失败，请检查服务器网络后重试: " + err.Error(), false
	default:
		return "Error", "OCI API 请求失败: " + err.Error(), false
	}
}
