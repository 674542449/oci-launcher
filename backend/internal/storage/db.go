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

	// Run auto migrations. Presets are generated per account type at request time now
	// (see api.ListPresets); the Preset table is kept only so old databases migrate cleanly.
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
	return db, nil
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
