package storage

import (
	"log"
	"time"

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

func seedInitialPresets(db *gorm.DB) {
	var count int64
	db.Model(&Preset{}).Count(&count)
	if count == 0 {
		initialPresets := []Preset{
			{
				Name:                "ARM 顶配主力单机 (4C / 24G / 100G)",
				Shape:               "VM.Standard.A1.Flex",
				OCPU:                4.0,
				MemoryInGBs:         24.0,
				BootVolumeSizeInGBs: 100,
				BootVolumeVPU:       120,
				LoginMode:           "root_key",
				EnableIPv6:          true,
			},
			{
				Name:                "ARM 双机平分方案 (2C / 12G / 50G)",
				Shape:               "VM.Standard.A1.Flex",
				OCPU:                2.0,
				MemoryInGBs:         12.0,
				BootVolumeSizeInGBs: 50,
				BootVolumeVPU:       120,
				LoginMode:           "root_key",
				EnableIPv6:          true,
			},
			{
				Name:                "ARM 四机矩阵方案 (1C / 6G / 50G)",
				Shape:               "VM.Standard.A1.Flex",
				OCPU:                1.0,
				MemoryInGBs:         6.0,
				BootVolumeSizeInGBs: 50,
				BootVolumeVPU:       120,
				LoginMode:           "root_key",
				EnableIPv6:          true,
			},
			{
				Name:                "AMD 永久免费微型机 (1C / 1G / 50G)",
				Shape:               "VM.Standard.E2.1.Micro",
				OCPU:                1.0,
				MemoryInGBs:         1.0,
				BootVolumeSizeInGBs: 50,
				BootVolumeVPU:       120,
				LoginMode:           "root_key",
				EnableIPv6:          true,
			},
			{
				Name:                "ARM 满配独享 (4C / 24G / 200G 占满全部免费额度)",
				Shape:               "VM.Standard.A1.Flex",
				OCPU:                4.0,
				MemoryInGBs:         24.0,
				BootVolumeSizeInGBs: 200,
				BootVolumeVPU:       120,
				LoginMode:           "root_key",
				EnableIPv6:          true,
			},
		}
		for _, p := range initialPresets {
			db.Create(&p)
		}
	}
}

// LogAudit records immutable audit log
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
