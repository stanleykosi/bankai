/**
 * @description
 * PostgreSQL connection manager using GORM.
 * Handles connection pooling and initialization.
 *
 * @dependencies
 * - gorm.io/gorm: ORM library
 * - gorm.io/driver/postgres: Postgres driver
 */

package db

import (
	"time"

	"github.com/bankai-project/backend/internal/config"
	"github.com/bankai-project/backend/internal/logger"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormLogger "gorm.io/gorm/logger"
)

// ConnectPostgres initializes the PostgreSQL connection
func ConnectPostgres(cfg *config.Config) (*gorm.DB, error) {
	// Configure GORM logger based on environment
	gormLogLevel := gormLogger.Error
	if cfg.Server.Env == "development" {
		gormLogLevel = gormLogger.Info
	} else if cfg.Server.Env == "staging" {
		gormLogLevel = gormLogger.Warn
	}

	db, err := gorm.Open(postgres.New(postgres.Config{
		DSN:                  cfg.DB.URL,
		PreferSimpleProtocol: true, // disable prepared statements to avoid stmtcache collisions in serverless envs
	}), &gorm.Config{
		Logger: gormLogger.Default.LogMode(gormLogLevel),
	})
	if err != nil {
		return nil, err
	}

	// Get generic database object to set connection pool params
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}

	// Set conservative connection pool settings for managed Postgres (e.g. Supabase)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetMaxOpenConns(10)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)

	logger.Info("✅ Connected to PostgreSQL")
	return db, nil
}
