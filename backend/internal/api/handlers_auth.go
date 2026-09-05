package api

import (
	"fmt"
	"net/http"
	"time"

	"oci-panel/internal/auth"
	"oci-panel/internal/cache"
	"oci-panel/internal/config"
	"oci-panel/internal/engine"
	"oci-panel/internal/security"
	"oci-panel/internal/storage"

	"github.com/gin-gonic/gin"
)

type InitAdminRequest struct {
	Username string `json:"username" binding:"required,min=3,max=32"`
	Password string `json:"password" binding:"required,min=8,max=64"`
}

type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type Verify2FARequest struct {
	TempToken string `json:"temp_token" binding:"required"`
	Code      string `json:"code" binding:"required,len=6"`
}

type ChangePasswordRequest struct {
	OldPassword string `json:"old_password" binding:"required"`
	NewPassword string `json:"new_password" binding:"required,min=8,max=64"`
}

// GetAuthStatus checks if an admin has been initialized
func GetAuthStatus(c *gin.Context) {
	var count int64
	storage.DB.Model(&storage.User{}).Count(&count)
	c.JSON(http.StatusOK, gin.H{
		"initialized": count > 0,
	})
}

// InitAdmin initializes the first admin user and returns TOTP setup
func InitAdmin(c *gin.Context) {
	var count int64
	storage.DB.Model(&storage.User{}).Count(&count)
	if count > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Admin already initialized"})
		return
	}

	var req InitAdminRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input parameters: " + err.Error()})
		return
	}

	passHash, err := security.HashPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password"})
		return
	}

	secret, qrURL, err := auth.GenerateTOTPSecret(req.Username)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate 2FA secret"})
		return
	}

	secretEnc, err := security.EncryptAES256GCM(secret, config.GlobalConfig.MasterKey)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to encrypt 2FA secret"})
		return
	}

	user := storage.User{
		Username:     req.Username,
		PasswordHash: passHash,
		TOTPSecret:   secretEnc,
		TOTPEnabled:  true,
		TokenVersion: 1,
	}

	if err := storage.DB.Create(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save user"})
		return
	}

	storage.LogAudit("INIT_ADMIN", req.Username, c.ClientIP(), c.GetHeader("User-Agent"), "First admin initialized with 2FA", "SUCCESS")

	c.JSON(http.StatusOK, gin.H{
		"message":     "Admin initialized successfully",
		"totp_secret": secret,
		"totp_qr_url": qrURL,
	})
}

// LoginStep1 validates username and password and returns temp token for 2FA
func LoginStep1(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Username and password required"})
		return
	}

	clientIP := c.ClientIP()
	ctx := c.Request.Context()

	var user storage.User
	err := storage.DB.Where("username = ?", req.Username).First(&user).Error
	if err != nil || !security.VerifyPassword(user.PasswordHash, req.Password) {
		isLocked, lockDur, attempts, _ := cache.RecordLoginFailure(ctx, clientIP, req.Username)
		storage.LogAudit("LOGIN_FAIL", req.Username, clientIP, c.GetHeader("User-Agent"), fmt.Sprintf("Failed password attempt %d", attempts), "FAILED")

		if isLocked {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": fmt.Sprintf("Too many failed attempts. Account locked for %v", lockDur),
			})
			return
		}
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid username or password"})
		return
	}

	// Generate temp token for 2FA step
	tempToken, err := auth.GenerateTemp2FAToken(&user, c.GetHeader("User-Agent"), clientIP)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to issue 2FA challenge token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"require_2fa": true,
		"temp_token":  tempToken,
	})
}

// Verify2FAStep2 validates the 6-digit TOTP code
func Verify2FAStep2(c *gin.Context) {
	var req Verify2FARequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "2FA code required"})
		return
	}

	ctx := c.Request.Context()
	clientIP := c.ClientIP()

	claims, err := auth.ValidateJWT(req.TempToken)
	if err != nil || claims.Is2FAVerified || claims.ID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "2FA token expired or invalid, please login again"})
		return
	}
	// A temp token is single-use: consumed on success or after too many wrong codes
	if cache.IsJTIBlacklisted(ctx, claims.ID) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "2FA token already used, please login again"})
		return
	}
	// The one-time code must be entered from the same device that entered the password
	if claims.DeviceFingerprint != security.GenerateDeviceFingerprint(c.GetHeader("User-Agent"), clientIP) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "2FA token does not belong to this device, please login again"})
		return
	}

	var user storage.User
	if err := storage.DB.First(&user, claims.UserID).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not found"})
		return
	}

	// 1. Decrypt TOTP secret
	secret, err := security.DecryptAES256GCM(user.TOTPSecret, config.GlobalConfig.MasterKey)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to decrypt 2FA secret"})
		return
	}

	// 2. Verify TOTP code; wrong codes count against the temp token and the client IP
	if !auth.VerifyTOTPCode(secret, req.Code) {
		failures, _ := cache.RecordTOTPFailure(ctx, claims.ID)
		isLocked, lockDur, attempts, _ := cache.RecordLoginFailure(ctx, clientIP, user.Username)
		storage.LogAudit("2FA_FAIL", user.Username, clientIP, c.GetHeader("User-Agent"), fmt.Sprintf("Wrong 2FA code (token failure %d, ip failure %d)", failures, attempts), "FAILED")
		if failures >= 5 {
			_ = cache.BlacklistJTI(ctx, claims.ID, 10*time.Minute)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Too many wrong 2FA codes, please login again"})
			return
		}
		if isLocked {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": fmt.Sprintf("Too many failed attempts. Account locked for %v", lockDur)})
			return
		}
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid 2FA code"})
		return
	}

	// 3. Atomically check and lock code against replay attacks
	if !cache.CheckAndSetTOTPUsed(ctx, user.ID, req.Code) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "This 2FA code was already consumed. Please wait for the next 30s code."})
		return
	}

	// Consume the temp token
	_ = cache.BlacklistJTI(ctx, claims.ID, 10*time.Minute)

	// Success! Generate full JWT
	token, _, err := auth.GenerateFullJWT(&user, c.GetHeader("User-Agent"), clientIP)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate session token"})
		return
	}

	// Clear login failure counter
	cache.ResetLoginFailures(ctx, clientIP)

	// HttpOnly, SameSite=Strict; Secure whenever the client came in over HTTPS
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie("oci_auth_token", token, 86400, "/", "", isRequestSecure(c), true)

	storage.LogAudit("LOGIN_SUCCESS", user.Username, c.ClientIP(), c.GetHeader("User-Agent"), "Logged in with 2FA successfully", "SUCCESS")

	c.JSON(http.StatusOK, gin.H{
		"message": "Login successful",
		"token":   token,
		"user": gin.H{
			"id":       user.ID,
			"username": user.Username,
		},
	})
}

// Logout revokes the session token
func Logout(c *gin.Context) {
	if jtiVal, exists := c.Get("tokenJTI"); exists {
		if jti, ok := jtiVal.(string); ok {
			_ = cache.BlacklistJTI(c.Request.Context(), jti, 24*time.Hour)
		}
	}

	// Clear Cookie
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie("oci_auth_token", "", -1, "/", "", isRequestSecure(c), true)

	c.JSON(http.StatusOK, gin.H{"message": "Logged out successfully"})
}

// ChangePassword changes user password and invalidates previous sessions
func ChangePassword(c *gin.Context) {
	userVal, _ := c.Get("currentUser")
	user := userVal.(*storage.User)

	var req ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid password request"})
		return
	}

	if !security.VerifyPassword(user.PasswordHash, req.OldPassword) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Old password incorrect"})
		return
	}

	newHash, err := security.HashPassword(req.NewPassword)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash new password"})
		return
	}

	storage.DB.Model(user).Updates(map[string]interface{}{
		"password_hash": newHash,
		"token_version": user.TokenVersion + 1, // Invalidates all prior tokens
	})

	storage.LogAudit("CHANGE_PASSWORD", user.Username, c.ClientIP(), c.GetHeader("User-Agent"), "Password changed successfully", "SUCCESS")

	c.JSON(http.StatusOK, gin.H{"message": "Password changed successfully, please login again"})
}

// TriggerPanicLockdown triggers emergency lockdown
func TriggerPanicLockdown(c *gin.Context) {
	userVal, _ := c.Get("currentUser")
	user := userVal.(*storage.User)

	engine.PanicLockdown()

	// Invalidate user token version
	storage.DB.Model(user).Update("token_version", user.TokenVersion+10)

	storage.LogAudit("PANIC_LOCKDOWN", user.Username, c.ClientIP(), c.GetHeader("User-Agent"), "EMERGENCY PANIC LOCKDOWN TRIGGERED", "LOCKED")

	c.JSON(http.StatusOK, gin.H{
		"message": "全站紧急锁定已触发！所有任务已停机，会话已吊销",
	})
}
