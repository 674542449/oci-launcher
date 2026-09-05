package api

import (
	"net/http"

	"oci-panel/internal/oci"

	"github.com/gin-gonic/gin"
)

// GetQuota returns dual-track account type detection, live compute usage, and 200GB storage progress
func GetQuota(c *gin.Context) {
	profile, ok := profileFromQuery(c)
	if !ok {
		return
	}

	summary, err := oci.GetLiveQuotaSummary(c.Request.Context(), &profile)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取配额失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"summary": summary,
	})
}

// GetTraffic returns monthly outbound traffic (BytesOut) vs 10 TB limit
func GetTraffic(c *gin.Context) {
	profile, ok := profileFromQuery(c)
	if !ok {
		return
	}

	traffic, err := oci.GetMonthlyOutboundTraffic(c.Request.Context(), &profile, profile.Region)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取流量数据失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"traffic": traffic,
	})
}
