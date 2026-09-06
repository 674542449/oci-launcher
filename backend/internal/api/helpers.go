package api

import (
	"net/http"
	"strconv"
	"strings"

	"oci-panel/internal/auth"
	"oci-panel/internal/storage"

	"github.com/gin-gonic/gin"
)

// parseID parses a numeric id from user input. GORM's First(&x, "<string>") treats a
// non-numeric string as a raw SQL condition, so ids must never reach it unparsed.
func parseID(raw string) (uint, bool) {
	n, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 32)
	if err != nil || n == 0 {
		return 0, false
	}
	return uint(n), true
}

// profileFromQuery loads the profile named by ?profile_id=. On failure it has already written
// the error response and returns ok=false.
func profileFromQuery(c *gin.Context) (storage.OCIProfile, bool) {
	return profileFromIDString(c, c.Query("profile_id"))
}

// profileFromParam loads the profile named by the :id route parameter.
func profileFromParam(c *gin.Context) (storage.OCIProfile, bool) {
	return profileFromIDString(c, c.Param("id"))
}

func profileFromIDString(c *gin.Context, raw string) (storage.OCIProfile, bool) {
	var profile storage.OCIProfile
	id, ok := parseID(raw)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "profile_id 无效"})
		return profile, false
	}
	if err := storage.DB.First(&profile, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Profile not found"})
		return profile, false
	}
	return profile, true
}

// isRequestSecure reports whether the client reached us over HTTPS (directly or via a proxy).
func isRequestSecure(c *gin.Context) bool {
	return auth.IsRequestSecure(c)
}
