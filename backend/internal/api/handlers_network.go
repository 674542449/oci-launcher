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

	view, err := oci.DescribeSecurityList(c.Request.Context(), &profile, profile.Region, secListID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取安全规则失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"rules": view.Ingress, "egress": view.Egress, "is_minimal": view.IsMinimal, "vcn_cidr": view.VcnCIDR})
}

type AddSecurityRuleRequest struct {
	ProfileID      uint   `json:"profile_id" binding:"required"`
	Region         string `json:"region" binding:"required"`
	SecurityListID string `json:"security_list_id" binding:"required"`
	Protocol       string `json:"protocol" binding:"required,oneof=tcp udp icmp all"`
	Source         string `json:"source" binding:"required,max=64"`
	PortMin        int    `json:"port_min" binding:"min=0,max=65535"`
	PortMax        int    `json:"port_max" binding:"min=0,max=65535"`
	Description    string `json:"description" binding:"max=255"`
	IsStateless    bool   `json:"is_stateless"`
}

type DeleteSecurityRuleRequest struct {
	ProfileID      uint   `json:"profile_id" binding:"required"`
	Region         string `json:"region" binding:"required"`
	SecurityListID string `json:"security_list_id" binding:"required"`
	Key            string `json:"key" binding:"required"`
}

// AddSecurityRule adds one ingress rule to a security list
func AddSecurityRule(c *gin.Context) {
	var req AddSecurityRuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "入参校验未通过: " + err.Error()})
		return
	}

	var profile storage.OCIProfile
	if err := storage.DB.First(&profile, req.ProfileID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Profile not found"})
		return
	}

	added, err := oci.AddSecurityRule(c.Request.Context(), &profile, req.Region, req.SecurityListID, oci.IngressRuleSpec{
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

	storage.LogAudit("FIREWALL_ADD_RULE", profile.Name, c.ClientIP(), c.GetHeader("User-Agent"),
		fmt.Sprintf("%s %s %d-%d on %s", req.Protocol, req.Source, req.PortMin, req.PortMax, req.SecurityListID), "SUCCESS")

	msg := "规则已添加"
	if !added {
		msg = "相同的规则已存在，未重复添加"
	}
	c.JSON(http.StatusOK, gin.H{"message": msg, "added": added})
}

// DeleteSecurityRule removes one ingress rule (identified by the key from the rule list)
func DeleteSecurityRule(c *gin.Context) {
	var req DeleteSecurityRuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	var profile storage.OCIProfile
	if err := storage.DB.First(&profile, req.ProfileID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Profile not found"})
		return
	}

	removed, err := oci.DeleteSecurityRule(c.Request.Context(), &profile, req.Region, req.SecurityListID, req.Key)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "删除规则失败: " + err.Error()})
		return
	}

	storage.LogAudit("FIREWALL_DELETE_RULE", profile.Name, c.ClientIP(), c.GetHeader("User-Agent"),
		fmt.Sprintf("%s on %s", req.Key, req.SecurityListID), "SUCCESS")

	c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("已删除 %d 条规则", removed)})
}
