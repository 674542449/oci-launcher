package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"oci-panel/internal/cache"
	"oci-panel/internal/storage"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

// DeriveKey derives a 32-byte AES key from master key
func DeriveKey(masterKey string) []byte {
	hash := sha256.Sum256([]byte(masterKey))
	return hash[:]
}

// EncryptAES256GCM encrypts plaintext using AES-256-GCM
func EncryptAES256GCM(plaintext string, masterKey string) (string, error) {
	key := DeriveKey(masterKey)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// DecryptAES256GCM decrypts ciphertext using AES-256-GCM
func DecryptAES256GCM(ciphertextB64 string, masterKey string) (string, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(ciphertextB64)
	if err != nil {
		return "", err
	}

	key := DeriveKey(masterKey)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", errors.New("ciphertext too short")
	}

	nonce, actualCiphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintextBytes, err := gcm.Open(nil, nonce, actualCiphertext, nil)
	if err != nil {
		return "", err
	}

	result := string(plaintextBytes)
	ZeroBytes(plaintextBytes) // Zero memory
	return result, nil
}

// ZeroBytes overwrites memory slice with zeros
func ZeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// HashPassword hashes password with bcrypt cost 12
func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	return string(bytes), err
}

// VerifyPassword securely verifies password
func VerifyPassword(hashedPassword, password string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
	return err == nil
}

// ConstantTimeCompare strings
func ConstantTimeCompare(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// GenerateDeviceFingerprint hashes the browser's User-Agent. The client IP is deliberately not
// part of it: Wi-Fi/mobile switches and rotating IPv6 privacy addresses were logging users out,
// while IP binding adds little on top of TOTP and the token blacklist. The parameter is kept so
// callers do not change.
func GenerateDeviceFingerprint(userAgent, _ string) string {
	raw := "ua|" + strings.TrimSpace(userAgent)
	hash := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(hash[:])
}

// SecurityHeadersMiddleware strips server banners and sets hardened CSP/security headers
func SecurityHeadersMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Server banner cloaking
		c.Header("Server", "")
		c.Header("X-Powered-By", "")

		// Hardened security headers
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Header("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data: https:; font-src 'self' data:; connect-src 'self' ws: wss:;")

		c.Next()
	}
}

// AntiScannerMiddleware blocks scanner tool User-Agents and automated bots
func AntiScannerMiddleware() gin.HandlerFunc {
	scannerKeywords := []string{
		"zgrab", "masscan", "nmap", "sqlmap", "nikto", "dirbuster",
		"wpscan", "censys", "shodan", "fofa", "acunetix", "nuclei",
		"gobuster", "nessus", "openvas",
	}

	return func(c *gin.Context) {
		clientIP := c.ClientIP()

		// Loopback and private/docker addresses (nginx, the tunnel container) are never banned:
		// banning them would lock every visitor out at once.
		if cache.IsImmuneIP(clientIP) {
			c.Next()
			return
		}

		// 1. Check IP blacklist
		if cache.IsIPBlacklisted(c.Request.Context(), clientIP) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "Access denied. IP is blacklisted."})
			return
		}

		// 2. Check scanner user agent
		ua := strings.ToLower(c.GetHeader("User-Agent"))
		for _, kw := range scannerKeywords {
			if strings.Contains(ua, kw) {
				_ = cache.BlacklistIP(c.Request.Context(), clientIP, 24*time.Hour, fmt.Sprintf("Scanner detected: %s", kw))
				storage.LogAudit("SCANNER_BLOCKED", "SYSTEM", clientIP, ua, fmt.Sprintf("Blocked scanner signature: %s", kw), "BLOCKED")
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "Forbidden signature detected."})
				return
			}
		}

		c.Next()
	}
}

// IPWhitelistMiddleware optionally enforces IP whitelist if configured
func IPWhitelistMiddleware(allowedIPs []string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if len(allowedIPs) == 0 {
			c.Next()
			return
		}

		clientIP := net.ParseIP(c.ClientIP())
		if clientIP == nil {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}

		allowed := false
		for _, allowedPattern := range allowedIPs {
			if strings.Contains(allowedPattern, "/") {
				_, ipNet, err := net.ParseCIDR(allowedPattern)
				if err == nil && ipNet.Contains(clientIP) {
					allowed = true
					break
				}
			} else {
				if clientIP.Equal(net.ParseIP(allowedPattern)) {
					allowed = true
					break
				}
			}
		}

		if !allowed {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "IP address not authorized"})
			return
		}

		c.Next()
	}
}

// HoneypotTrapHandler blocks anyone probing honeypot trap routes
func HoneypotTrapHandler(c *gin.Context) {
	clientIP := c.ClientIP()
	if cache.IsImmuneIP(clientIP) {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	ua := c.GetHeader("User-Agent")
	path := c.Request.URL.Path

	_ = cache.BlacklistIP(c.Request.Context(), clientIP, 24*time.Hour, fmt.Sprintf("Honeypot trap triggered: %s", path))
	storage.LogAudit("HONEYPOT_TRIGGERED", "SYSTEM", clientIP, ua, fmt.Sprintf("Attempted to access honey route %s", path), "BANNED_24H")

	c.AbortWithStatus(http.StatusNotFound)
}
