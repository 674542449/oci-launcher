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

	// A1 allowances used by the zero-cost guard, chosen by the account's effective type:
	//   - Always Free tenancy (FREE_*): 2 OCPU / 12 GB
	//   - Upgraded PAYG tenancy (PAYG_*): 4 OCPU / 24 GB
	// Storage (200 GB) and E2.1.Micro count (2) are the same for both.
	FreeA1OCPU     float64
	FreeA1MemoryGB float64
	PaygA1OCPU     float64
	PaygA1MemoryGB float64
	FreeStorageGB  int64
	FreeMicroCount int

	// Console-style names (instance-YYYYMMDD-HHMM …) are stamped in this IANA zone.
	NameTimezone string
	// Queued creation: stop after RetryMaxDays (0 = never) and never issue more than
	// RetryMaxLaunchesPerDay real LaunchInstance calls per task and day (0 = unlimited).
	// Between capacity checks the worker sleeps a random CapacityPollMinSecs..MaxSecs.
	RetryMaxDays           int
	RetryMaxLaunchesPerDay int
	CapacityPollMinSecs    int
	CapacityPollMaxSecs    int
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
		PaygA1OCPU:     getEnvFloat("PAYG_A1_OCPU", 4),
		PaygA1MemoryGB: getEnvFloat("PAYG_A1_MEMORY_GB", 24),
		FreeStorageGB:  int64(getEnvInt("FREE_STORAGE_GB", 200)),
		FreeMicroCount: getEnvInt("FREE_MICRO_COUNT", 2),

		NameTimezone:           getEnv("NAME_TIMEZONE", "Asia/Tokyo"),
		RetryMaxDays:           getEnvIntAllowZero("RETRY_MAX_DAYS", 7),
		RetryMaxLaunchesPerDay: getEnvIntAllowZero("RETRY_MAX_LAUNCHES_PER_DAY", 30),
		CapacityPollMinSecs:    getEnvInt("CAPACITY_POLL_MIN_SECS", 180),
		CapacityPollMaxSecs:    getEnvInt("CAPACITY_POLL_MAX_SECS", 300),
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

// getEnvIntAllowZero is getEnvInt where an explicit 0 is a valid value ("unlimited").
func getEnvIntAllowZero(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(val)); err == nil && n >= 0 {
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
