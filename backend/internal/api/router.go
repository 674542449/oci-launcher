package api

import (
	"log"
	"os"
	"strings"

	"oci-panel/internal/auth"
	"oci-panel/internal/config"
	"oci-panel/internal/security"

	"github.com/gin-gonic/gin"
)

// trustedProxyCIDRs returns the proxies whose forwarded-IP headers are believed.
// Default: the private ranges Docker networks use (the nginx container in docker-compose).
// Override with TRUSTED_PROXIES="cidr,cidr" or set it to "none" when the backend is reached directly.
func trustedProxyCIDRs() []string {
	raw := strings.TrimSpace(os.Getenv("TRUSTED_PROXIES"))
	if strings.EqualFold(raw, "none") {
		return nil
	}
	if raw == "" {
		return []string{"127.0.0.1/32", "::1/128", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"}
	}
	var out []string
	for _, p := range strings.Split(raw, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func SetupRouter() *gin.Engine {
	if config.GlobalConfig.AppEnv == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()
	r.Use(gin.Recovery())

	// Client IP: only X-Real-IP (which nginx overwrites with the real peer address) from trusted
	// proxies. X-Forwarded-For is deliberately ignored because clients can prepend their own value.
	r.RemoteIPHeaders = []string{"X-Real-IP"}
	if err := r.SetTrustedProxies(trustedProxyCIDRs()); err != nil {
		log.Printf("[Router] invalid TRUSTED_PROXIES: %v", err)
	}

	// Security & Anti-Scan Middlewares
	r.Use(security.SecurityHeadersMiddleware())
	r.Use(security.AntiScannerMiddleware())
	r.Use(security.IPWhitelistMiddleware(config.GlobalConfig.AllowedIPs))

	// Honeypot Trap Routes to auto-ban scanners
	honeyRoutes := []string{
		"/wp-login.php", "/wp-admin", "/phpmyadmin", "/pma",
		"/.env", "/.git/config", "/actuator/health", "/swagger-ui.html",
		"/api/v1/debug", "/xmlrpc.php",
	}
	for _, route := range honeyRoutes {
		r.GET(route, security.HoneypotTrapHandler)
		r.POST(route, security.HoneypotTrapHandler)
	}

	api := r.Group("/api")
	{
		// Public Auth Routes
		authGroup := api.Group("/auth")
		{
			authGroup.GET("/status", GetAuthStatus)
			authGroup.POST("/init", InitAdmin)
			authGroup.POST("/login", LoginStep1)
			authGroup.POST("/2fa/verify", Verify2FAStep2)
		}

		// Protected Routes
		protected := api.Group("")
		protected.Use(auth.RequireAuth())
		{
			// Auth
			protected.POST("/auth/logout", Logout)
			protected.POST("/auth/change-password", ChangePassword)
			protected.POST("/auth/panic-lockdown", TriggerPanicLockdown)

			// Profiles
			protected.GET("/profiles", ListProfiles)
			protected.POST("/profiles/import-raw", ImportRawProfile)
			protected.POST("/profiles/update", UpdateProfile)
			protected.DELETE("/profiles/delete/:id", DeleteProfile)
			protected.GET("/profiles/health/:id", CheckSingleHealth)
			protected.POST("/profiles/sync-local", SyncLocalProfiles)

			// Quota & Traffic
			protected.GET("/quota", GetQuota)
			protected.GET("/quota/traffic", GetTraffic)

			// Instances
			protected.GET("/instances", ListInstances)
			protected.POST("/instances/action", PerformInstanceAction)
			protected.POST("/instances/terminate", TerminateInstance)
			protected.POST("/instances/resize", ResizeInstance)
			protected.POST("/instances/rotate-ip", RotatePublicIP)
			protected.POST("/instances/probe-ip", ProbeIP)
			protected.POST("/instances/attach-ipv6", AttachIPv6)
			protected.POST("/instances/update-tags", UpdateInstanceTags)
			protected.GET("/instances/metrics", InstanceMetrics)
			// Per-instance firewall (NSG on the primary VNIC)
			protected.GET("/instances/firewall", GetInstanceFirewall)
			protected.POST("/instances/firewall/enable", EnableInstanceFirewall)
			protected.POST("/instances/firewall/disable", DisableInstanceFirewall)
			protected.POST("/instances/firewall/rules/add", AddInstanceFirewallRule)
			protected.POST("/instances/firewall/rules/delete", DeleteInstanceFirewallRule)
			protected.POST("/instances/firewall/allow-all", AllowAllInstanceFirewall)
			protected.POST("/instances/firewall/allow-cloudflare", AllowCloudflareInstanceFirewall)
			protected.POST("/instances/firewall/clear", ClearInstanceFirewall)

			// Storage
			protected.GET("/storage/boot-volumes", ListBootVolumes)
			protected.POST("/storage/boot-volumes/resize", ResizeBootVolume)
			protected.POST("/storage/boot-volumes/backup", CreateBootVolumeBackup)
			protected.GET("/storage/block-volumes", ListBlockVolumes)
			protected.POST("/storage/block-volumes/create", CreateBlockVolume)
			protected.GET("/storage/buckets", ListBuckets)
			protected.POST("/storage/buckets/create", CreateBucket)
			protected.DELETE("/storage/buckets/delete", DeleteBucket)

			// Network & Firewall
			protected.GET("/network/vcns", ListVCNs)
			protected.GET("/network/subnets", ListSubnets)
			protected.GET("/network/ads", ListAvailabilityDomains)
			protected.POST("/network/create-default-vcn", CreateDefaultVCN)
			protected.GET("/network/security-rules", ListSecurityRules)
			protected.POST("/network/security-rules/add", AddSecurityRule)
			protected.POST("/network/security-rules/delete", DeleteSecurityRule)
			protected.POST("/network/reset-minimal", ResetSecurityListMinimal)

			// Tasks
			protected.GET("/tasks", ListTasks)
			protected.POST("/tasks/create", CreateTask)
			protected.POST("/tasks/start/:id", StartExistingTask)
			protected.POST("/tasks/stop/:id", StopExistingTask)
			protected.DELETE("/tasks/delete/:id", DeleteExistingTask)
			protected.GET("/tasks/presets", ListPresets)
			protected.GET("/tasks/images", ListDynamicUbuntuImages)
			protected.GET("/tasks/attempts/:id", ListTaskAttempts)

			// Audit Logs & Settings
			protected.GET("/audit-logs", GetAuditLogs)
			protected.GET("/settings", GetSettings)
			protected.POST("/settings/save", SaveSetting)
			protected.POST("/settings/test-telegram", TestTelegram)

			// Saved SSH public keys
			protected.GET("/ssh-keys", ListSSHKeys)
			protected.POST("/ssh-keys", CreateSSHKey)
			protected.DELETE("/ssh-keys/:id", DeleteSSHKey)
			protected.POST("/ssh-keys/default/:id", SetDefaultSSHKey)

			// Security: bans
			protected.GET("/security/bans", ListBans)
			protected.POST("/security/unban", UnbanIP)
		}
	}

	// WebSocket log streaming: same session cookie as the API (sent with the upgrade request)
	r.GET("/ws/logs/:task_id", auth.RequireAuth(), HandleTaskLogWS)

	return r
}
