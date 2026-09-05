package storage

import (
	"fmt"
	"log"
	"time"

	"oci-panel/internal/config"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

func InitDB(dsn string) (*gorm.DB, error) {
	var db *gorm.DB
	var err error

	// Retry connection for database readiness
	for i := 0; i < 10; i++ {
		db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Warn),
		})
		if err == nil {
			break
		}
		log.Printf("Waiting for PostgreSQL connection... retry %d/10: %v", i+1, err)
		time.Sleep(2 * time.Second)
	}

	if err != nil {
		return nil, err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}

	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(50)
	sqlDB.SetConnMaxLifetime(time.Hour)

	// Run auto migrations
	err = db.AutoMigrate(
		&User{},
		&OCIProfile{},
		&LaunchTask{},
		&TaskAttempt{},
		&Preset{},
		&AuditLog{},
		&SystemSetting{},
	)
	if err != nil {
		return nil, err
	}

	DB = db

	seedInitialPresets(db)

	return db, nil
}

// seedInitialPresets keeps the quick presets inside the configured Always Free allowance.
// Presets are not user-editable, so stale seeds from older versions (4C/24G) are dropped and
// the set is re-created whenever it is empty.
func seedInitialPresets(db *gorm.DB) {
	freeOCPU, freeMem, freeStorage := 2.0, 12.0, int64(200)
	if cfg := config.GlobalConfig; cfg != nil {
		if cfg.FreeA1OCPU > 0 {
			freeOCPU = cfg.FreeA1OCPU
		}
		if cfg.FreeA1MemoryGB > 0 {
			freeMem = cfg.FreeA1MemoryGB
		}
		if cfg.FreeStorageGB > 0 {
			freeStorage = cfg.FreeStorageGB
		}
	}

	// Remove presets that would fail the zero-cost guard
	db.Where("shape LIKE ? AND (ocpu > ? OR memory_in_gbs > ?)", "%A1.Flex%", freeOCPU, freeMem).Delete(&Preset{})

	var count int64
	db.Model(&Preset{}).Count(&count)
	if count > 0 {
		return
	}

	halfOCPU := freeOCPU / 2
	halfMem := freeMem / 2
	if halfOCPU < 1 {
		halfOCPU, halfMem = freeOCPU, freeMem
	}

	presets := []Preset{
		{
			Name:                fmt.Sprintf("ARM 满额单机 (%.0fC / %.0fG / 100G)", freeOCPU, freeMem),
			Shape:               "VM.Standard.A1.Flex",
			OCPU:                freeOCPU,
			MemoryInGBs:         freeMem,
			BootVolumeSizeInGBs: 100,
			BootVolumeVPU:       120,
			LoginMode:           "root_key",
			EnableIPv6:          true,
		},
		{
			Name:                fmt.Sprintf("ARM 双机平分 (%.0fC / %.0fG / 50G)", halfOCPU, halfMem),
			Shape:               "VM.Standard.A1.Flex",
			OCPU:                halfOCPU,
			MemoryInGBs:         halfMem,
			BootVolumeSizeInGBs: 50,
			BootVolumeVPU:       120,
			LoginMode:           "root_key",
			EnableIPv6:          true,
		},
		{
			Name:                "AMD 微型机 (1C / 1G / 50G)",
			Shape:               "VM.Standard.E2.1.Micro",
			OCPU:                1.0,
			MemoryInGBs:         1.0,
			BootVolumeSizeInGBs: 50,
			BootVolumeVPU:       120,
			LoginMode:           "root_key",
			EnableIPv6:          true,
		},
		{
			Name:                fmt.Sprintf("ARM 满额独享 (%.0fC / %.0fG / %dG 占满存储额度)", freeOCPU, freeMem, freeStorage),
			Shape:               "VM.Standard.A1.Flex",
			OCPU:                freeOCPU,
			MemoryInGBs:         freeMem,
			BootVolumeSizeInGBs: freeStorage,
			BootVolumeVPU:       120,
			LoginMode:           "root_key",
			EnableIPv6:          true,
		},
	}
	for _, p := range presets {
		preset := p
		db.Create(&preset)
	}
}

// LogAudit records an immutable audit log entry
func LogAudit(action, operator, clientIP, userAgent, details, status string) {
	if DB == nil {
		return
	}
	audit := AuditLog{
		Action:    action,
		Operator:  operator,
		ClientIP:  clientIP,
		UserAgent: userAgent,
		Details:   details,
		Status:    status,
		CreatedAt: time.Now(),
	}
	DB.Create(&audit)
}
