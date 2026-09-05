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

// seedInitialPresets keeps the quick presets inside the configured A1 allowances:
// free tenancies get 2 OCPU / 12 GB, upgraded PAYG tenancies 4 OCPU / 24 GB. Presets are not
// user-editable, so anything above the PAYG allowance is dropped and the set is re-created
// whenever it is empty. The zero-cost guard still rejects a PAYG preset on a free account.
func seedInitialPresets(db *gorm.DB) {
	freeOCPU, freeMem := 2.0, 12.0
	paygOCPU, paygMem := 4.0, 24.0
	freeStorage := int64(200)
	if cfg := config.GlobalConfig; cfg != nil {
		if cfg.FreeA1OCPU > 0 {
			freeOCPU = cfg.FreeA1OCPU
		}
		if cfg.FreeA1MemoryGB > 0 {
			freeMem = cfg.FreeA1MemoryGB
		}
		if cfg.PaygA1OCPU > 0 {
			paygOCPU = cfg.PaygA1OCPU
		}
		if cfg.PaygA1MemoryGB > 0 {
			paygMem = cfg.PaygA1MemoryGB
		}
		if cfg.FreeStorageGB > 0 {
			freeStorage = cfg.FreeStorageGB
		}
	}
	maxOCPU, maxMem := freeOCPU, freeMem
	if paygOCPU > maxOCPU {
		maxOCPU = paygOCPU
	}
	if paygMem > maxMem {
		maxMem = paygMem
	}

	// Remove presets no account type could ever launch for free
	db.Where("shape LIKE ? AND (ocpu > ? OR memory_in_gbs > ?)", "%A1.Flex%", maxOCPU, maxMem).Delete(&Preset{})

	var count int64
	db.Model(&Preset{}).Count(&count)
	if count > 0 {
		return
	}

	a1 := func(name string, ocpu, mem float64, boot int64) Preset {
		return Preset{
			Name:                name,
			Shape:               "VM.Standard.A1.Flex",
			OCPU:                ocpu,
			MemoryInGBs:         mem,
			BootVolumeSizeInGBs: boot,
			BootVolumeVPU:       120,
			LoginMode:           "root_key",
			EnableIPv6:          true,
		}
	}

	presets := []Preset{
		a1(fmt.Sprintf("升级号 ARM 满配 (%.0fC / %.0fG / 100G)", paygOCPU, paygMem), paygOCPU, paygMem, 100),
		a1(fmt.Sprintf("免费号 ARM 满配 (%.0fC / %.0fG / 50G)", freeOCPU, freeMem), freeOCPU, freeMem, 50),
		a1(fmt.Sprintf("ARM 小机 (%.0fC / %.0fG / 50G)", freeOCPU/2, freeMem/2), freeOCPU/2, freeMem/2, 50),
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
		a1(fmt.Sprintf("升级号 ARM 满配独享 (%.0fC / %.0fG / %dG 占满存储额度)", paygOCPU, paygMem, freeStorage), paygOCPU, paygMem, freeStorage),
	}
	if freeOCPU/2 < 1 {
		presets = append(presets[:2], presets[3:]...)
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
