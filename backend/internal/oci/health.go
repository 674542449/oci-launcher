package oci

import (
	"context"
	"fmt"

	"oci-panel/internal/storage"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/core"
)

type AccountHealthResult struct {
	ProfileID   uint   `json:"profile_id"`
	Status      string `json:"status"` // "Active", "Banned", "Invalid", "Error"
	IsHealthy   bool   `json:"is_healthy"`
	Message     string `json:"message"`
	CheckedAt   string `json:"checked_at"`
}

// CheckSingleAccountHealth executes single-account health inspection using ListInstances
// STRICT RULE: Only single account, no batch!
func CheckSingleAccountHealth(ctx context.Context, profile *storage.OCIProfile) (*AccountHealthResult, error) {
	computeClient, err := GetComputeClient(profile, profile.Region)
	if err != nil {
		return &AccountHealthResult{
			ProfileID: profile.ID,
			Status:    "Error",
			IsHealthy: false,
			Message:   fmt.Sprintf("凭据初始化失败: %v", err),
		}, nil
	}

	req := core.ListInstancesRequest{
		CompartmentId: common.String(profile.TenancyOCID),
		Limit:         common.Int(1),
	}

	resp, err := computeClient.ListInstances(ctx, req)
	if err != nil {
		errStr := strings.ToLower(err.Error())
		status := "Error"
		msg := fmt.Sprintf("OCI API 请求失败: %v", err)

		// Check for ban/suspension/authentication revocation signatures
		if strings.Contains(errStr, "notauthenticated") ||
			strings.Contains(errStr, "authorization failed") ||
			strings.Contains(errStr, "invalid key") {
			status = "Invalid"
			msg = "API 私钥或用户凭据无效/已被吊销"
		} else if strings.Contains(errStr, "tenancydisabled") ||
			strings.Contains(errStr, "userdisabled") ||
			strings.Contains(errStr, "account is suspended") ||
			strings.Contains(errStr, "tenant has been deleted") {
			status = "Banned"
			msg = "【严重警告】租户已停用或被封号 (TenancyDisabled / Suspended)"
		}

		// Update profile status in database
		storage.DB.Model(profile).Updates(map[string]interface{}{
			"status":         status,
			"status_message": msg,
		})

		// If banned/invalid, automatically pause any running tasks for this profile
		if status == "Banned" || status == "Invalid" {
			storage.DB.Model(&storage.LaunchTask{}).
				Where("profile_id = ? AND status = ?", profile.ID, "running").
				Updates(map[string]interface{}{
					"status":       "stopped",
					"last_message": fmt.Sprintf("账号状态异常 (%s)，系统已自动熔断保护", status),
				})
		}

		return &AccountHealthResult{
			ProfileID: profile.ID,
			Status:    status,
			IsHealthy: false,
			Message:   msg,
		}, nil
	}

	// Success
	count := len(resp.Items)
	msg := fmt.Sprintf("账号正常活跃，连接顺畅 (探测到活跃实例数: %d)", count)
	storage.DB.Model(profile).Updates(map[string]interface{}{
		"status":         "Active",
		"status_message": msg,
	})

	return &AccountHealthResult{
		ProfileID: profile.ID,
		Status:    "Active",
		IsHealthy: true,
		Message:   msg,
	}, nil
}
