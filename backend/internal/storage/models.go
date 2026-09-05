package storage

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type User struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	Username     string    `gorm:"size:64;uniqueIndex;not null" json:"username"`
	PasswordHash string    `gorm:"size:255;not null" json:"-"`
	TOTPSecret   string    `gorm:"type:text;not null" json:"-"`
	TOTPEnabled  bool      `gorm:"default:false" json:"totp_enabled"`
	TokenVersion int       `gorm:"default:1" json:"token_version"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type OCIProfile struct {
	ID                  uint      `gorm:"primaryKey" json:"id"`
	Name                string    `gorm:"size:64;uniqueIndex;not null" json:"name"`
	TenancyOCID         string    `gorm:"size:255;not null" json:"tenancy_ocid"`
	UserOCID            string    `gorm:"size:255;not null" json:"user_ocid"`
	Fingerprint         string    `gorm:"size:128;not null" json:"fingerprint"`
	Region              string    `gorm:"size:64;not null" json:"region"`
	PrivateKeyEnc       string    `gorm:"type:text;not null" json:"-"`
	AccountTypeOverride string    `gorm:"size:16;default:'auto'" json:"account_type_override"` // auto, free, payg
	DetectedType        string    `gorm:"size:32;default:'UNKNOWN'" json:"detected_type"`       // FREE_TIER, PAYG, PROMOTION
	DetectionReason     string    `gorm:"type:text" json:"detection_reason"`
	Status              string    `gorm:"size:32;default:'Active'" json:"status"`               // Active, Banned, Invalid
	StatusMessage       string    `gorm:"type:text" json:"status_message"`
	Tags                string    `gorm:"type:text" json:"tags"`                               // comma-separated tags e.g. "Main,PAYG,Tokyo"
	Notes               string    `gorm:"type:text" json:"notes"`                              // personal remarks/email
	IsActive            bool      `gorm:"default:true" json:"is_active"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type LaunchTask struct {
	ID                   uuid.UUID      `gorm:"type:uuid;primaryKey" json:"id"`
	ProfileID            uint           `gorm:"index;not null" json:"profile_id"`
	InstanceName         string         `gorm:"size:128;not null" json:"instance_name"`
	Shape                string         `gorm:"size:64;not null" json:"shape"` // VM.Standard.A1.Flex, VM.Standard.E2.1.Micro
	OCPU                 float64        `gorm:"not null" json:"ocpu"`
	MemoryInGBs          float64        `gorm:"not null" json:"memory_in_gbs"`
	BootVolumeSizeInGBs  int64          `gorm:"not null;default:50" json:"boot_volume_size_in_gbs"`
	BootVolumeVPU        int64          `gorm:"not null;default:120" json:"boot_volume_vpu"` // Default 120 VPU Ultra High Performance
	Region               string         `gorm:"size:64;not null" json:"region"`
	ADList               string         `gorm:"type:text;not null" json:"ad_list"` // JSON array
	ImageOCID            string         `gorm:"size:255;not null" json:"image_ocid"`
	SubnetOCID           string         `gorm:"size:255;not null" json:"subnet_ocid"`
	LoginMode            string         `gorm:"size:32;default:'root_key'" json:"login_mode"` // root_key, root_password
	SSHAuthorizedKeys    string         `gorm:"type:text" json:"ssh_authorized_keys"`
	RootPasswordEnc      string         `gorm:"type:text" json:"-"`
	AssignPublicIP       bool           `gorm:"default:true" json:"assign_public_ip"`
	EnableIPv6           bool           `gorm:"default:false" json:"enable_ipv6"`
	CloudInitScript      string         `gorm:"type:text" json:"cloud_init_script"`
	Status               string         `gorm:"size:32;default:'idle'" json:"status"` // idle, running, success, failed, stopped
	RetryIntervalSecs    int            `gorm:"default:60" json:"retry_interval_secs"`
	MaxRetries           int            `gorm:"default:0" json:"max_retries"`
	CurrentRetries       int            `gorm:"default:0" json:"current_retries"`
	LastAttemptAt        *time.Time     `json:"last_attempt_at"`
	LastMessage          string         `gorm:"type:text" json:"last_message"`
	SuccessInstanceOCID  string         `gorm:"size:255" json:"success_instance_ocid"`
	SuccessPublicIP      string         `gorm:"size:64" json:"success_public_ip"`
	SuccessIPv6          string         `gorm:"size:128" json:"success_ipv6"`
	CreatedAt            time.Time      `json:"created_at"`
	UpdatedAt            time.Time      `json:"updated_at"`
	DeletedAt            gorm.DeletedAt `gorm:"index" json:"-"`
}

type TaskAttempt struct {
	ID              uint64    `gorm:"primaryKey" json:"id"`
	TaskID          uuid.UUID `gorm:"type:uuid;index;not null" json:"task_id"`
	AttemptNum      int       `gorm:"not null" json:"attempt_num"`
	Region          string    `gorm:"size:64;not null" json:"region"`
	AD              string    `gorm:"size:64;not null" json:"ad"`
	Status          string    `gorm:"size:32;not null" json:"status"` // capacity_full, rate_limited, success, fatal_error
	ResponseMessage string    `gorm:"type:text" json:"response_message"`
	DurationMs      int64     `gorm:"not null" json:"duration_ms"`
	CreatedAt       time.Time `json:"created_at"`
}

type Preset struct {
	ID                  uint      `gorm:"primaryKey" json:"id"`
	Name                string    `gorm:"size:64;not null" json:"name"`
	Shape               string    `gorm:"size:64;not null" json:"shape"`
	OCPU                float64   `gorm:"not null" json:"ocpu"`
	MemoryInGBs         float64   `gorm:"not null" json:"memory_in_gbs"`
	BootVolumeSizeInGBs int64     `gorm:"not null" json:"boot_volume_size_in_gbs"`
	BootVolumeVPU       int64     `gorm:"not null;default:120" json:"boot_volume_vpu"`
	LoginMode           string    `gorm:"size:32;default:'root_key'" json:"login_mode"`
	SSHAuthorizedKeys   string    `gorm:"type:text" json:"ssh_authorized_keys"`
	EnableIPv6          bool      `gorm:"default:false" json:"enable_ipv6"`
	CreatedAt           time.Time `json:"created_at"`
}

type AuditLog struct {
	ID        uint64    `gorm:"primaryKey" json:"id"`
	Action    string    `gorm:"size:64;not null" json:"action"`
	Operator  string    `gorm:"size:64;not null" json:"operator"`
	ClientIP  string    `gorm:"size:64;not null" json:"client_ip"`
	UserAgent string    `gorm:"size:255" json:"user_agent"`
	Details   string    `gorm:"type:text" json:"details"`
	Status    string    `gorm:"size:32;not null" json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type SystemSetting struct {
	Key       string    `gorm:"size:64;primaryKey" json:"key"`
	Value     string    `gorm:"type:text;not null" json:"value"`
	UpdatedAt time.Time `json:"updated_at"`
}
