package api

import (
	"fmt"
	"net/http"
	"strings"

	"oci-panel/internal/oci"
	"oci-panel/internal/storage"

	"github.com/gin-gonic/gin"
)

// Per-instance firewall (one NSG on the instance's primary VNIC).

type instanceFirewallRequest struct {
	ProfileID uint   `json:"profile_id" binding:"required"`
	Region    string `json:"region" binding:"required"`
	OCID      string `json:"ocid" binding:"required"`
}

type nsgRuleAddRequest struct {
	ProfileID   uint   `json:"profile_id" binding:"required"`
	Region      string `json:"region" binding:"required"`
	NSGID       string `json:"nsg_id" binding:"required"`
	Protocol    string `json:"protocol" binding:"required,oneof=tcp udp icmp all"`
	Source      string `json:"source" binding:"required,max=64"`
	PortMin     int    `json:"port_min" binding:"min=0,max=65535"`
	PortMax     int    `json:"port_max" binding:"min=0,max=65535"`
	Description string `json:"description" binding:"max=255"`
	IsStateless bool   `json:"is_stateless"`
}

type nsgRuleDeleteRequest struct {
	ProfileID uint   `json:"profile_id" binding:"required"`
	Region    string `json:"region" binding:"required"`
	NSGID     string `json:"nsg_id" binding:"required"`
	RuleID    string `json:"rule_id" binding:"required"`
}

type instanceFirewallDisableRequest struct {
	ProfileID uint   `json:"profile_id" binding:"required"`
	Region    string `json:"region" binding:"required"`
	OCID      string `json:"ocid" binding:"required"`
	NSGID     string `json:"nsg_id" binding:"required"`
}

func loadProfile(c *gin.Context, id uint) (storage.OCIProfile, bool) {
	var profile storage.OCIProfile
	if err := storage.DB.First(&profile, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Profile not found"})
		return profile, false
	}
	return profile, true
}

// GetInstanceFirewall returns the instance's NSG (if any) and its ingress rules.
func GetInstanceFirewall(c *gin.Context) {
	profile, ok := profileFromQuery(c)
	if !ok {
		return
	}
	ocid := strings.TrimSpace(c.Query("ocid"))
	if !strings.HasPrefix(ocid, "ocid1.instance.") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ocid 无效"})
		return
	}
	fw, err := oci.GetInstanceFirewall(c.Request.Context(), &profile, profile.Region, ocid)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "读取实例防火墙失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"firewall": fw})
}

// EnableInstanceFirewall creates the instance's NSG and attaches it to the primary VNIC.
func EnableInstanceFirewall(c *gin.Context) {
	var req instanceFirewallRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "入参校验未通过: " + err.Error()})
		return
	}
	profile, ok := loadProfile(c, req.ProfileID)
	if !ok {
		return
	}
	fw, err := oci.EnsureInstanceNSG(c.Request.Context(), &profile, req.Region, req.OCID)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "启用实例防火墙失败: " + err.Error()})
		return
	}
	storage.LogAudit("INSTANCE_FIREWALL_ENABLE", profile.Name, c.ClientIP(), c.GetHeader("User-Agent"), req.OCID, "SUCCESS")
	c.JSON(http.StatusOK, gin.H{"message": "已为该实例启用专属防火墙", "firewall": fw})
}

// AddInstanceFirewallRule adds one ingress rule to the instance's NSG.
func AddInstanceFirewallRule(c *gin.Context) {
	var req nsgRuleAddRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "入参校验未通过: " + err.Error()})
		return
	}
	profile, ok := loadProfile(c, req.ProfileID)
	if !ok {
		return
	}
	added, err := oci.AddNSGRule(c.Request.Context(), &profile, req.Region, req.NSGID, oci.IngressRuleSpec{
		Protocol:    req.Protocol,
		Source:      req.Source,
		PortMin:     req.PortMin,
		PortMax:     req.PortMax,
		Description: req.Description,
		IsStateless: req.IsStateless,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "添加规则失败: " + err.Error()})
		return
	}
	storage.LogAudit("INSTANCE_FIREWALL_ADD_RULE", profile.Name, c.ClientIP(), c.GetHeader("User-Agent"),
		fmt.Sprintf("%s %s %d-%d on %s", req.Protocol, req.Source, req.PortMin, req.PortMax, req.NSGID), "SUCCESS")
	msg := "规则已添加"
	if !added {
		msg = "相同的规则已存在，未重复添加"
	}
	c.JSON(http.StatusOK, gin.H{"message": msg, "added": added})
}

// DeleteInstanceFirewallRule removes one rule from the instance's NSG.
func DeleteInstanceFirewallRule(c *gin.Context) {
	var req nsgRuleDeleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "入参校验未通过: " + err.Error()})
		return
	}
	profile, ok := loadProfile(c, req.ProfileID)
	if !ok {
		return
	}
	if err := oci.DeleteNSGRule(c.Request.Context(), &profile, req.Region, req.NSGID, req.RuleID); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "删除规则失败: " + err.Error()})
		return
	}
	storage.LogAudit("INSTANCE_FIREWALL_DELETE_RULE", profile.Name, c.ClientIP(), c.GetHeader("User-Agent"), req.RuleID+" on "+req.NSGID, "SUCCESS")
	c.JSON(http.StatusOK, gin.H{"message": "规则已删除"})
}

// DisableInstanceFirewall detaches the NSG from the instance and deletes it.
func DisableInstanceFirewall(c *gin.Context) {
	var req instanceFirewallDisableRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "入参校验未通过: " + err.Error()})
		return
	}
	profile, ok := loadProfile(c, req.ProfileID)
	if !ok {
		return
	}
	deleted, err := oci.RemoveInstanceNSG(c.Request.Context(), &profile, req.Region, req.OCID, req.NSGID)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "移除实例防火墙失败: " + err.Error()})
		return
	}
	storage.LogAudit("INSTANCE_FIREWALL_DISABLE", profile.Name, c.ClientIP(), c.GetHeader("User-Agent"), req.NSGID+" from "+req.OCID, "SUCCESS")
	msg := "专属防火墙已移除"
	if !deleted {
		msg = "已从该实例解绑；安全组仍被其他网卡使用，未删除"
	}
	c.JSON(http.StatusOK, gin.H{"message": msg, "deleted": deleted})
}
