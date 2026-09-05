package api

import (
	"fmt"
	"net/http"

	"oci-panel/internal/oci"
	"oci-panel/internal/storage"

	"github.com/gin-gonic/gin"
)

type VCNRequest struct {
	ProfileID uint   `json:"profile_id" binding:"required"`
	Region    string `json:"region" binding:"required"`
}

type FirewallActionRequest struct {
	ProfileID      uint   `json:"profile_id" binding:"required"`
	Region         string `json:"region" binding:"required"`
	SecurityListID string `json:"security_list_id" binding:"required"`
}

// ListVCNs lists VCNs
func ListVCNs(c *gin.Context) {
	profile, ok := profileFromQuery(c)
	if !ok {
		return
	}

	vcns, err := oci.ListVCNs(c.Request.Context(), &profile, profile.Region)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取 VCN 失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"vcns": vcns})
}

// ListSubnets lists subnets for a VCN
func ListSubnets(c *gin.Context) {
	vcnID := c.Query("vcn_id")

	profile, ok := profileFromQuery(c)
	if !ok {
		return
	}

	subnets, err := oci.ListSubnets(c.Request.Context(), &profile, profile.Region, vcnID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取子网失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"subnets": subnets})
}

// CreateDefaultVCN creates recommended VCN with all required components
func CreateDefaultVCN(c *gin.Context) {
	var req VCNRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	var profile storage.OCIProfile
	if err := storage.DB.First(&profile, req.ProfileID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Profile not found"})
		return
	}

	vcnID, subnetID, warnings, err := oci.CreateRecommendedVCN(c.Request.Context(), &profile, req.Region)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建推荐网络失败: " + err.Error(), "warnings": warnings})
		return
	}

	storage.LogAudit("CREATE_VCN", profile.Name, c.ClientIP(), c.GetHeader("User-Agent"), "Created recommended VCN "+vcnID, "SUCCESS")

	msg := "推荐 VCN 与公共子网创建成功"
	if len(warnings) > 0 {
		msg += "（有 " + fmt.Sprint(len(warnings)) + " 条提示）"
	}
	c.JSON(http.StatusOK, gin.H{
		"message":   msg,
		"vcn_id":    vcnID,
		"subnet_id": subnetID,
		"warnings":  warnings,
	})
}

// ListAvailabilityDomains returns the AD names of the profile's region (or ?region=)
func ListAvailabilityDomains(c *gin.Context) {
	profile, ok := profileFromQuery(c)
	if !ok {
		return
	}
	region := c.Query("region")
	if region == "" {
		region = profile.Region
	}

	names, err := oci.ListAvailabilityDomainNames(c.Request.Context(), &profile, region)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取可用区失败: " + err.Error()})
		return
	}

	ads := make([]gin.H, 0, len(names))
	for _, n := range names {
		ads = append(ads, gin.H{"name": n})
	}
	c.JSON(http.StatusOK, gin.H{"ads": ads, "region": region})
}

// ListSecurityRules lists ingress rules of security list
func ListSecurityRules(c *gin.Context) {
	secListID := c.Query("security_list_id")

	profile, ok := profileFromQuery(c)
	if !ok {
		return
	}

	rules, err := oci.ListSecurityRules(c.Request.Context(), &profile, profile.Region, secListID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取安全规则失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"rules": rules})
}

// AllowAllFirewall adds 0.0.0.0/0 & ::/0 allow all rules
func AllowAllFirewall(c *gin.Context) {
	var req FirewallActionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	var profile storage.OCIProfile
	if err := storage.DB.First(&profile, req.ProfileID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Profile not found"})
		return
	}

	err := oci.AllowAllFirewallRules(c.Request.Context(), &profile, req.Region, req.SecurityListID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "放行所有规则失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "已成功添加 IPv4 与 IPv6 全端口全协议放行规则！"})
}

// ClearAllFirewall clears all ingress rules
func ClearAllFirewall(c *gin.Context) {
	var req FirewallActionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	var profile storage.OCIProfile
	if err := storage.DB.First(&profile, req.ProfileID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Profile not found"})
		return
	}

	err := oci.ClearAllFirewallRules(c.Request.Context(), &profile, req.Region, req.SecurityListID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "清空规则失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "已成功清空所有入站安全规则"})
}

// AllowCloudflareCDN allows Cloudflare official IPv4 & IPv6 CIDRs on 80/443
func AllowCloudflareCDN(c *gin.Context) {
	var req FirewallActionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	var profile storage.OCIProfile
	if err := storage.DB.First(&profile, req.ProfileID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Profile not found"})
		return
	}

	err := oci.AllowCloudflareCDNIPs(c.Request.Context(), &profile, req.Region, req.SecurityListID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "放通 Cloudflare CDN 失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "已成功批量放行 Cloudflare 官方 IPv4/IPv6 节点的 80/443 入站！"})
}
