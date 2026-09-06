package auth

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"oci-panel/internal/cache"
	"oci-panel/internal/config"
	"oci-panel/internal/security"
	"oci-panel/internal/storage"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

type CustomClaims struct {
	UserID            uint   `json:"uid"`
	Username          string `json:"usr"`
	TokenVersion      int    `json:"ver"`
	DeviceFingerprint string `json:"dfp"`
	Is2FAVerified     bool   `json:"fa"`
	SessionStart      int64  `json:"sst,omitempty"` // unix time of the original login (sliding session)
	jwt.RegisteredClaims
}

const (
	// SessionCookieName carries the session token as an HttpOnly cookie.
	SessionCookieName = "oci_auth_token"
	// SessionTokenLifetime is the idle cap: a token that goes unused this long expires.
	SessionTokenLifetime = 7 * 24 * time.Hour
	// sessionAbsoluteMax is the hard cap counted from the original login; renewal stops there.
	sessionAbsoluteMax = 30 * 24 * time.Hour
	// sessionRenewAfter: a token older than this is reissued on its next use, so an active user
	// is never logged out by the idle cap.
	sessionRenewAfter = time.Hour
)

func getJWTSecret() []byte {
	hash := sha256.Sum256([]byte(config.GlobalConfig.MasterKey + "|jwt-signing-salt"))
	return hash[:]
}

// GenerateFullJWT issues the authenticated session token for a fresh login.
func GenerateFullJWT(user *storage.User, userAgent, clientIP string) (string, string, error) {
	token, jti, _, err := issueSessionToken(user, userAgent, clientIP, time.Now())
	return token, jti, err
}

// issueSessionToken signs a session token that expires after SessionTokenLifetime, but never
// later than sessionAbsoluteMax after the session's original login.
func issueSessionToken(user *storage.User, userAgent, clientIP string, sessionStart time.Time) (string, string, time.Time, error) {
	now := time.Now()
	exp := now.Add(SessionTokenLifetime)
	if hard := sessionStart.Add(sessionAbsoluteMax); hard.Before(exp) {
		exp = hard
	}
	if !exp.After(now.Add(time.Minute)) {
		return "", "", time.Time{}, errors.New("session reached its absolute lifetime")
	}

	jti := uuid.New().String()
	claims := CustomClaims{
		UserID:            user.ID,
		Username:          user.Username,
		TokenVersion:      user.TokenVersion,
		DeviceFingerprint: security.GenerateDeviceFingerprint(userAgent, clientIP),
		Is2FAVerified:     true,
		SessionStart:      sessionStart.Unix(),
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        jti,
			Issuer:    "oci-panel-backend",
			Subject:   fmt.Sprintf("%d", user.ID),
			ExpiresAt: jwt.NewNumericDate(exp),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signedToken, err := token.SignedString(getJWTSecret())
	return signedToken, jti, exp, err
}

func sessionStartOf(claims *CustomClaims) time.Time {
	if claims.SessionStart > 0 {
		return time.Unix(claims.SessionStart, 0)
	}
	if claims.IssuedAt != nil {
		return claims.IssuedAt.Time
	}
	return time.Now()
}

// renewSessionIfDue reissues the session token once it is older than sessionRenewAfter. The new
// token goes out as the cookie and in the X-Refreshed-Token header (the SPA stores it); the old
// one stays valid until its own expiry so requests already in flight are not cut off.
func renewSessionIfDue(c *gin.Context, user *storage.User, claims *CustomClaims) {
	if claims.IssuedAt == nil || time.Since(claims.IssuedAt.Time) < sessionRenewAfter {
		return
	}
	token, _, exp, err := issueSessionToken(user, c.GetHeader("User-Agent"), c.ClientIP(), sessionStartOf(claims))
	if err != nil {
		return // past the absolute cap: let the current token run out
	}
	SetSessionCookie(c, token, int(time.Until(exp).Seconds()))
	c.Header("X-Refreshed-Token", token)
}

// IsRequestSecure reports whether the client reached us over HTTPS (directly or via a proxy).
func IsRequestSecure(c *gin.Context) bool {
	if c.Request.TLS != nil {
		return true
	}
	return strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https")
}

// SetSessionCookie writes the HttpOnly, SameSite=Strict session cookie (Secure over HTTPS).
func SetSessionCookie(c *gin.Context, token string, maxAge int) {
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(SessionCookieName, token, maxAge, "/", "", IsRequestSecure(c), true)
}

// ClearSessionCookie deletes the session cookie.
func ClearSessionCookie(c *gin.Context) {
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(SessionCookieName, "", -1, "/", "", IsRequestSecure(c), true)
}

// GenerateTemp2FAToken generates temporary token for 2FA challenge step
func GenerateTemp2FAToken(user *storage.User, userAgent, clientIP string) (string, error) {
	dfp := security.GenerateDeviceFingerprint(userAgent, clientIP)
	claims := CustomClaims{
		UserID:            user.ID,
		Username:          user.Username,
		TokenVersion:      user.TokenVersion,
		DeviceFingerprint: dfp,
		Is2FAVerified:     false,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuid.New().String(),
			Issuer:    "oci-panel-backend",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(5 * time.Minute)), // 5 min for 2FA input
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(getJWTSecret())
}

// ValidateJWT validates token and ensures HS256 algorithm
func ValidateJWT(tokenStr string) (*CustomClaims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &CustomClaims{}, func(t *jwt.Token) (interface{}, error) {
		// Strict algorithm check: MUST be HS256 to prevent 'none' and confusion attacks
		if t.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing algorithm: %v", t.Header["alg"])
		}
		return getJWTSecret(), nil
	})

	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*CustomClaims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token claims")
	}

	return claims, nil
}

// GenerateTOTPSecret generates new TOTP secret for user setup
func GenerateTOTPSecret(username string) (secret string, qrURL string, err error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "OCI-Panel",
		AccountName: username,
	})
	if err != nil {
		return "", "", err
	}
	return key.Secret(), key.URL(), nil
}

// VerifyTOTPCode verifies 6-digit TOTP code with 1-period (30s) skew tolerance
func VerifyTOTPCode(secret, code string) bool {
	valid, err := totp.ValidateCustom(code, secret, time.Now().UTC(), totp.ValidateOpts{
		Period:    30,
		Skew:      1,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	})
	return err == nil && valid
}

// RequireAuth middleware verifies the JWT and the browser fingerprint, then slides the session
func RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		var tokenStr string

		// Check HttpOnly Cookie first
		if cookie, err := c.Cookie(SessionCookieName); err == nil && cookie != "" {
			tokenStr = cookie
		} else {
			// Check Authorization Header fallback
			authHeader := c.GetHeader("Authorization")
			if strings.HasPrefix(authHeader, "Bearer ") {
				tokenStr = strings.TrimPrefix(authHeader, "Bearer ")
			}
		}

		if tokenStr == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication required"})
			return
		}

		claims, err := ValidateJWT(tokenStr)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid or expired token"})
			return
		}

		if !claims.Is2FAVerified {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "2FA verification required"})
			return
		}

		// Check JTI blacklist
		if cache.IsJTIBlacklisted(c.Request.Context(), claims.ID) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Token has been revoked"})
			return
		}

		// Check Device Fingerprint binding
		expectedDfp := security.GenerateDeviceFingerprint(c.GetHeader("User-Agent"), c.ClientIP())
		if !security.ConstantTimeCompare(claims.DeviceFingerprint, expectedDfp) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Device signature mismatch. Session terminated."})
			return
		}

		// Check user in database and token version
		var user storage.User
		if err := storage.DB.First(&user, claims.UserID).Error; err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "User no longer exists"})
			return
		}

		if user.TokenVersion != claims.TokenVersion {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Token version outdated, please login again"})
			return
		}

		c.Set("currentUser", &user)
		c.Set("tokenJTI", claims.ID)
		renewSessionIfDue(c, &user, claims)
		c.Next()
	}
}
