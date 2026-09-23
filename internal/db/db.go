package db

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/Giancarlos/guardrails/internal/models"
)

const (
	// GuardrailsDir is the directory name for guardrails data
	GuardrailsDir = ".guardrails"
	// DBFileName is the database filename within the guardrails directory
	DBFileName = "db.sqlite"
	// SchemaVersion is the current schema version
	SchemaVersion = "1"
)

var (
	db     *gorm.DB
	dbMu   sync.RWMutex
	dbOnce sync.Once
)

// InitDB initializes the database connection and runs migrations
func InitDB(dbPath string) (*gorm.DB, error) {
	// Ensure the directory exists
	dir := filepath.Dir(dbPath)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create database directory: %w", err)
		}
	}

	// Configure GORM with silent logger for production
	config := &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	}

	// Open SQLite database
	database, err := gorm.Open(sqlite.Open(dbPath), config)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Configure connection pool for SQLite
	// Note: SQLite supports multiple readers but only one writer.
	// Setting a small pool allows concurrent reads within transactions.
	sqlDB, err := database.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}
	sqlDB.SetMaxOpenConns(5)
	sqlDB.SetMaxIdleConns(2)

	// SQLite performance optimizations
	pragmas := []struct {
		sql  string
		desc string
	}{
		{"PRAGMA journal_mode=WAL", "enable WAL mode"},           // Better concurrency
		{"PRAGMA busy_timeout=5000", "set busy timeout"},         // Wait on locks
		{"PRAGMA synchronous=NORMAL", "set synchronous mode"},    // Safe with WAL, faster
		{"PRAGMA cache_size=-64000", "set cache size"},           // 64MB cache
		{"PRAGMA temp_store=MEMORY", "set temp store to memory"}, // Temp tables in RAM
	}
	for _, p := range pragmas {
		if err := database.Exec(p.sql).Error; err != nil {
			return nil, fmt.Errorf("failed to %s: %w", p.desc, err)
		}
	}

	// Run migrations
	if err := runMigrations(database); err != nil {
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	dbMu.Lock()
	db = database
	dbMu.Unlock()
	return database, nil
}

// runMigrations runs all database migrations
func runMigrations(database *gorm.DB) error {
	err := database.AutoMigrate(
		&models.Task{},
		&models.Dependency{},
		&models.Config{},
		&models.Gate{},
		&models.GateTaskLink{},
		&models.GateRun{},
		&models.Template{},
		&models.TaskHistory{},
		&models.GitHubIssueLink{},
		&models.Skill{},
		&models.Agent{},
		&models.TaskSkillLink{},
		&models.TaskAgentLink{},
		&models.Checkpoint{},
		&models.Handoff{},
		&models.TimeEntry{},
		&models.Hook{},
	)
	if err != nil {
		return err
	}

	// Backfill: mark tasks as synced if they have a github_issue_links entry
	if err := database.Exec(`
		UPDATE tasks SET synced = true
		WHERE id IN (SELECT task_id FROM github_issue_links)
		AND synced = false
	`).Error; err != nil {
		return fmt.Errorf("failed to backfill synced field: %w", err)
	}

	return nil
}

// GetDB returns the current database connection
func GetDB() *gorm.DB {
	dbMu.RLock()
	defer dbMu.RUnlock()
	return db
}

// SetDB sets the database connection (used for testing)
func SetDB(database *gorm.DB) {
	dbMu.Lock()
	defer dbMu.Unlock()
	db = database
}

// CloseDB closes the database connection
func CloseDB() error {
	dbMu.Lock()
	defer dbMu.Unlock()

	if db == nil {
		return nil
	}

	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}

	err = sqlDB.Close()
	db = nil
	return err
}

// FindProjectRoot searches for a guardrails project root
func FindProjectRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %w", err)
	}

	dir := cwd
	for {
		guardrailsPath := filepath.Join(dir, GuardrailsDir)
		if info, err := os.Stat(guardrailsPath); err == nil && info.IsDir() {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("not a guardrails project (no %s/ found)", GuardrailsDir)
		}
		dir = parent
	}
}

// GetDefaultDBPath returns the default database path for the current project
func GetDefaultDBPath() (string, error) {
	root, err := FindProjectRoot()
	if err != nil {
		cwd, cwdErr := os.Getwd()
		if cwdErr != nil {
			return "", cwdErr
		}
		return filepath.Join(cwd, GuardrailsDir, DBFileName), nil
	}
	return filepath.Join(root, GuardrailsDir, DBFileName), nil
}

// EnsureInitialized checks if the database is initialized
// Uses sync.Once to prevent race conditions during concurrent initialization
func EnsureInitialized() error {
	dbMu.RLock()
	isNil := db == nil
	dbMu.RUnlock()

	if !isNil {
		return nil
	}

	var initErr error
	dbOnce.Do(func() {
		dbPath, err := GetDefaultDBPath()
		if err != nil {
			initErr = err
			return
		}
		if _, err := os.Stat(dbPath); os.IsNotExist(err) {
			initErr = fmt.Errorf("guardrails not initialized. Run 'gur init' first")
			return
		}
		_, initErr = InitDB(dbPath)
	})
	return initErr
}

// SetConfig sets a configuration value
func SetConfig(key, value string) error {
	config := models.Config{Key: key, Value: value}
	return db.Save(&config).Error
}

// GetConfig gets a configuration value
func GetConfig(key string) (string, error) {
	var config models.Config
	err := db.Where("key = ?", key).First(&config).Error
	if err != nil {
		return "", err
	}
	return config.Value, nil
}

// GetTaskByID retrieves a task by its ID
func GetTaskByID(id string) (*models.Task, error) {
	var task models.Task
	if err := GetDB().Where("id = ?", id).First(&task).Error; err != nil {
		return nil, err
	}
	return &task, nil
}

// GetGateByID retrieves a gate by its ID
func GetGateByID(id string) (*models.Gate, error) {
	var gate models.Gate
	if err := GetDB().Where("id = ?", id).First(&gate).Error; err != nil {
		return nil, err
	}
	return &gate, nil
}
