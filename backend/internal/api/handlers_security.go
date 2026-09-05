package api

import (
	"net"
	"net/http"
	"strings"

	"oci-panel/internal/cache"
	"oci-panel/internal/storage"

	"github.com/gin-gonic/gin"
)

// ListBans returns the IPs currently banned by the login lockout, scanner or honeypot rules
func ListBans(c *gin.Context) {
	bans, err := cache.ListBannedIPs(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取封禁列表失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"bans": bans, "your_ip": c.ClientIP()})
}

// UnbanIP lifts a ban and resets the failure counters of one IP
func UnbanIP(c *gin.Context) {
	var req struct {
		IP string `json:"ip" binding:"required,max=64"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}
	ip := strings.TrimSpace(req.IP)
	if net.ParseIP(ip) == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "IP 地址无效"})
		return
	}

	if err := cache.UnbanIP(c.Request.Context(), ip); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "解封失败: " + err.Error()})
		return
	}

	storage.LogAudit("UNBAN_IP", "admin", c.ClientIP(), c.GetHeader("User-Agent"), "Unbanned "+ip, "SUCCESS")
	c.JSON(http.StatusOK, gin.H{"message": ip + " 已解封"})
}
