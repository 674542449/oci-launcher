package api

import (
	"oci-panel/internal/auth"
	"oci-panel/internal/config"
	"oci-panel/internal/security"

	"github.com/gin-gonic/gin"
)

func SetupRouter() *gin.Engine {
	if config.GlobalConfig.AppEnv == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()
	r.Use(gin.Recovery())

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
			protected.POST("/network/create-default-vcn", CreateDefaultVCN)
			protected.GET("/network/security-rules", ListSecurityRules)
			protected.POST("/network/allow-all", AllowAllFirewall)
			protected.POST("/network/clear-all", ClearAllFirewall)
			protected.POST("/network/allow-cloudflare", AllowCloudflareCDN)

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
		}
	}

	// WebSocket Log Streaming
	r.GET("/ws/logs/:task_id", HandleTaskLogWS)

	return r
}
