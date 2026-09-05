package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	AppPort    int
	AppEnv     string
	DBDSN      string
	RedisAddr  string
	RedisPass  string
	MasterKey  string
	AllowedIPs []string
	DataDir    string

	// Always Free allowances used by the zero-cost guard. Defaults follow
	// https://docs.oracle.com/en-us/iaas/Content/FreeTier/freetier_topic-Always_Free_Resources.htm
	// (A1.Flex: 1,500 OCPU-hours + 9,000 GB-hours per month = 2 OCPU / 12 GB continuously;
	// 2 x E2.1.Micro; 200 GB block/boot storage). Override with FREE_* env vars if Oracle changes them.
	FreeA1OCPU     float64
	FreeA1MemoryGB float64
	FreeStorageGB  int64
	FreeMicroCount int
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

		FreeA1OCPU:     getEnvFloat("FREE_A1_OCPU", 2),
		FreeA1MemoryGB: getEnvFloat("FREE_A1_MEMORY_GB", 12),
		FreeStorageGB:  int64(getEnvInt("FREE_STORAGE_GB", 200)),
		FreeMicroCount: getEnvInt("FREE_MICRO_COUNT", 2),
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

func getEnvInt(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(val)); err == nil && n > 0 {
			return n
		}
	}
	return defaultVal
}

func getEnvFloat(key string, defaultVal float64) float64 {
	if val := os.Getenv(key); val != "" {
		if f, err := strconv.ParseFloat(strings.TrimSpace(val), 64); err == nil && f > 0 {
			return f
		}
	}
	return defaultVal
}
