package api

import (
	"net/http"

	"oci-panel/internal/oci"

	"github.com/gin-gonic/gin"
)

// GetBilling returns the account's cost summary (Usage API, cached 30 minutes; ?refresh=1
// bypasses the cache).
func GetBilling(c *gin.Context) {
	profile, ok := profileFromQuery(c)
	if !ok {
		return
	}
	force := c.Query("refresh") == "1"
	summary, err := oci.GetBillingSummary(c.Request.Context(), &profile, force)
	if err != nil {
		status, _, isSvc := oci.ServiceErrorInfo(err)
		msg := "读取账单数据失败: " + err.Error()
		if isSvc && (status == 401 || status == 403 || status == 404) {
			msg += "（该 API 用户需要 usage-reports 的读取权限：Allow group <组> to read usage-reports in tenancy）"
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": msg})
		return
	}
	c.JSON(http.StatusOK, gin.H{"billing": summary})
}
