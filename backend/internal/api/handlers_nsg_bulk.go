package api

import (
	"fmt"
	"net/http"

	"oci-panel/internal/oci"
	"oci-panel/internal/storage"

	"github.com/gin-gonic/gin"
)

// Shortcuts on the per-instance NSG, and the "minimal" reset of the shared subnet list.

type nsgActionRequest struct {
	ProfileID uint   `json:"profile_id" binding:"required"`
	Region    string `json:"region" binding:"required"`
	NSGID     string `json:"nsg_id" binding:"required"`
}

func bindNSGAction(c *gin.Context) (nsgActionRequest, storage.OCIProfile, bool) {
	var req nsgActionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "入参校验未通过: " + err.Error()})
		return req, storage.OCIProfile{}, false
	}
	profile, ok := loadProfile(c, req.ProfileID)
	return req, profile, ok
}

// AllowAllInstanceFirewall adds allow-all IPv4 and IPv6 ingress rules to the instance's NSG.
func AllowAllInstanceFirewall(c *gin.Context) {
	req, profile, ok := bindNSGAction(c)
	if !ok {
		return
	}
	added, err := oci.AllowAllNSG(c.Request.Context(), &profile, req.Region, req.NSGID)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "放通全部失败: " + err.Error()})
		return
	}
	storage.LogAudit("INSTANCE_FIREWALL_ALLOW_ALL", profile.Name, c.ClientIP(), c.GetHeader("User-Agent"), req.NSGID, "SUCCESS")
	c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("已放通全部端口与协议（新增 %d 条规则）", added), "added": added})
}

// AllowCloudflareInstanceFirewall allows Cloudflare's ranges on 80/443 in the instance's NSG.
func AllowCloudflareInstanceFirewall(c *gin.Context) {
	req, profile, ok := bindNSGAction(c)
	if !ok {
		return
	}
	added, err := oci.AllowCloudflareNSG(c.Request.Context(), &profile, req.Region, req.NSGID)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "放通 Cloudflare 失败: " + err.Error()})
		return
	}
	storage.LogAudit("INSTANCE_FIREWALL_ALLOW_CF", profile.Name, c.ClientIP(), c.GetHeader("User-Agent"), req.NSGID, "SUCCESS")
	c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("已放通 Cloudflare 节点的 80/443（新增 %d 条规则）", added), "added": added})
}

// ClearInstanceFirewall removes every ingress rule from the instance's NSG.
func ClearInstanceFirewall(c *gin.Context) {
	req, profile, ok := bindNSGAction(c)
	if !ok {
		return
	}
	removed, err := oci.ClearNSGRules(c.Request.Context(), &profile, req.Region, req.NSGID)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "清空规则失败: " + err.Error()})
		return
	}
	storage.LogAudit("INSTANCE_FIREWALL_CLEAR", profile.Name, c.ClientIP(), c.GetHeader("User-Agent"), req.NSGID, "SUCCESS")
	c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("已清空 %d 条规则", removed), "removed": removed})
}

// ResetSecurityListMinimal rewrites the subnet security list to the minimal baseline (two ICMP
// rules in, everything out) so per-instance firewalls are the only thing that opens ports.
func ResetSecurityListMinimal(c *gin.Context) {
	var req FirewallActionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "入参校验未通过: " + err.Error()})
		return
	}
	profile, ok := loadProfile(c, req.ProfileID)
	if !ok {
		return
	}
	if err := oci.ResetSecurityListToMinimal(c.Request.Context(), &profile, req.Region, req.SecurityListID); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "恢复最小规则失败: " + err.Error()})
		return
	}
	storage.LogAudit("FIREWALL_RESET_MINIMAL", profile.Name, c.ClientIP(), c.GetHeader("User-Agent"), req.SecurityListID, "SUCCESS")
	c.JSON(http.StatusOK, gin.H{"message": "安全列表已恢复为最小规则：入站仅保留两条 ICMP，端口由各实例的专属防火墙决定"})
}
