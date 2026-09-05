package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	AppPort     int
	AppEnv      string
	DBDSN       string
	RedisAddr   string
	RedisPass   string
	MasterKey   string
	AllowedIPs  []string
	DataDir     string
}

var GlobalConfig *Config

func LoadConfig() *Config {
	portStr := getEnv("APP_PORT", "8080")
	port, err := strconv.Atoi(portStr)
	if err != nil {
		port = 8080
	}

	allowedIPsRaw := getEnv("ALLOWED_IPS", "")
	var allowedIPs []string
	if allowedIPsRaw != "" {
		for _, ip := range strings.Split(allowedIPsRaw, ",") {
			trimmed := strings.TrimSpace(ip)
			if trimmed != "" {
				allowedIPs = append(allowedIPs, trimmed)
			}
		}
	}

	cfg := &Config{
		AppPort:    port,
		AppEnv:     getEnv("APP_ENV", "production"),
		DBDSN:      getEnv("DB_DSN", "postgres://oci_admin:OciPanelSecPass998!@localhost:5432/oci_panel?sslmode=disable"),
		RedisAddr:  getEnv("REDIS_ADDR", "localhost:6379"),
		RedisPass:  getEnv("REDIS_PASSWORD", "OciRedisPass998!"),
		MasterKey:  getEnv("MASTER_KEY", "SuperMasterSecretKey32BytesLong!"),
		AllowedIPs: allowedIPs,
		DataDir:    getEnv("DATA_DIR", "./data"),
	}

	GlobalConfig = cfg
	return cfg
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
