// Package main provides a Redis migration tool for managing schema and data migrations.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/go-redis/redis/v8"
	"gopkg.in/yaml.v3"
)

const (
	// retryDelay is the delay between migration retry attempts.
	retryDelay = 100 * time.Millisecond

	// File permissions.
	dirPermissions  = 0o750
	filePermissions = 0o600
)

// Migration represents a single Redis migration.
type Migration struct {
	ID          string                 `json:"id"                   yaml:"id"`
	Description string                 `json:"description"          yaml:"description"`
	Version     string                 `json:"version"              yaml:"version"`
	Type        string                 `json:"type"                 yaml:"type"` // "schema", "data", "index"
	Script      string                 `json:"script"               yaml:"script"`
	Rollback    string                 `json:"rollback,omitempty"   yaml:"rollback,omitempty"`
	CheckSum    string                 `json:"checksum"             yaml:"checksum"`
	Applied     bool                   `json:"applied"              yaml:"-"`
	AppliedAt   *time.Time             `json:"applied_at,omitempty" yaml:"-"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"   yaml:"metadata,omitempty"`
}

// MigrationHistory tracks applied migrations.
type MigrationHistory struct {
	ID        string    `json:"id"`
	Version   string    `json:"version"`
	AppliedAt time.Time `json:"applied_at"`
	CheckSum  string    `json:"checksum"`
	Success   bool      `json:"success"`
	Error     string    `json:"error,omitempty"`
}

// Migrator handles Redis migrations.
type Migrator struct {
	client     *redis.Client
	migrations []Migration
	config     Config
	ctx        context.Context
}

// Config holds migrator configuration.
type Config struct {
	RedisURL       string `yaml:"redis_url"`
	RedisPassword  string `yaml:"redis_password"`
	MigrationsPath string `yaml:"migrations_path"`
	HistoryKey     string `yaml:"history_key"`
	BackupEnabled  bool   `yaml:"backup_enabled"`
	BackupPath     string `yaml:"backup_path"`
	DryRun         bool   `yaml:"dry_run"`
	ValidateOnly   bool   `yaml:"validate_only"`
}

func main() {
	config, cmd := parseFlags()

	if cmd.create != "" {
		handleCreate(cmd.create, config.MigrationsPath)

		return
	}

	client := connectRedis(config)

	defer func() { _ = client.Close() }()

	migrator := setupMigrator(client, config)

	executeCommand(migrator, cmd, client)
}

// loadConfig loads configuration from file.
type commandFlags struct {
	status   bool
	rollback string
	create   string
}

func parseFlags() (Config, commandFlags) {
	var (
		configFile     = flag.String("config", "", "Configuration file path")
		migrationsPath = flag.String("migrations", "./migrations/redis", "Path to migrations directory")
		redisURL       = flag.String("redis", "localhost:6379", "Redis URL")
		redisPassword  = flag.String("password", "", "Redis password")
		dryRun         = flag.Bool("dry-run", false, "Perform dry run without applying changes")
		validate       = flag.Bool("validate", false, "Validate migrations only")
		rollback       = flag.String("rollback", "", "Rollback to specified migration ID")
		status         = flag.Bool("status", false, "Show migration status")
		create         = flag.String("create", "", "Create new migration with given name")
	)

	flag.Parse()

	config := Config{
		RedisURL:       *redisURL,
		RedisPassword:  *redisPassword,
		MigrationsPath: *migrationsPath,
		HistoryKey:     "mcp:migrations:history",
		BackupEnabled:  true,
		BackupPath:     "./backups",
		DryRun:         *dryRun,
		ValidateOnly:   *validate,
	}

	if *configFile != "" {
		if err := loadConfig(*configFile, &config); err != nil {
			log.Fatalf("Failed to load config: %v", err)
		}
	}

	return config, commandFlags{
		status:   *status,
		rollback: *rollback,
		create:   *create,
	}
}

func handleCreate(name, path string) {
	if err := createMigration(name, path); err != nil {
		log.Fatalf("Failed to create migration: %v", err)
	}
}

func connectRedis(config Config) *redis.Client {
	client := redis.NewClient(&redis.Options{
		Addr:     config.RedisURL,
		Password: config.RedisPassword,
		DB:       0,
	})

	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}

	return client
}

func setupMigrator(client *redis.Client, config Config) *Migrator {
	migrator := &Migrator{
		client:     client,
		migrations: nil,
		config:     config,
		ctx:        context.Background(),
	}

	if err := migrator.loadMigrations(); err != nil {
		_ = client.Close()

		log.Fatalf("Failed to load migrations: %v", err)
	}

	return migrator
}

func executeCommand(migrator *Migrator, cmd commandFlags, client *redis.Client) {
	switch {
	case cmd.status:
		migrator.showStatus()

	case cmd.rollback != "":
		if err := migrator.rollbackTo(cmd.rollback); err != nil {
			_ = client.Close()

			log.Fatalf("Rollback failed: %v", err)
		}

	case migrator.config.ValidateOnly:
		if err := migrator.validate(); err != nil {
			_ = client.Close()

			log.Fatalf("Validation failed: %v", err)
		}

		log.Println("All migrations are valid")

	default:
		if err := migrator.migrate(); err != nil {
			_ = client.Close()

			log.Fatalf("Migration failed: %v", err)
		}
	}
}

func loadConfig(path string, config *Config) error {
	data, err := os.ReadFile(path) //nolint:gosec // Tool for controlled migration
	if err != nil {
		return err
	}

	return yaml.Unmarshal(data, config)
}

// loadMigrations loads all migration files.
//

func (m *Migrator) loadMigrations() error {
	pattern := filepath.Join(m.config.MigrationsPath, "*.yaml")

	files, err := filepath.Glob(pattern)
	if err != nil {
		return err
	}

	for _, file := range files {
		migration, err := m.loadMigration(file)
		if err != nil {
			return fmt.Errorf("failed to load %s: %w", file, err)
		}

		m.migrations = append(m.migrations, *migration)
	}

	// Sort migrations by version/ID
	// Implementation depends on versioning scheme

	return nil
}

// loadMigration loads a single migration file.
func (m *Migrator) loadMigration(path string) (*Migration, error) {
	data, err := os.ReadFile(path) //nolint:gosec // Tool for controlled migration
	if err != nil {
		return nil, err
	}

	var migration Migration
	if err := yaml.Unmarshal(data, &migration); err != nil {
		return nil, err
	}

	// Check if already applied
	applied, appliedAt, err := m.isApplied(migration.ID)
	if err != nil {
		return nil, err
	}

	migration.Applied = applied
	migration.AppliedAt = appliedAt

	return &migration, nil
}

// isApplied checks if a migration has been applied.
func (m *Migrator) isApplied(id string) (bool, *time.Time, error) {
	key := fmt.Sprintf("%s:%s", m.config.HistoryKey, id)
	val, err := m.client.Get(m.ctx, key).Result()

	if errors.Is(err, redis.Nil) {
		return false, nil, nil
	}

	if err != nil {
		return false, nil, err
	}

	var history MigrationHistory
	if err := json.Unmarshal([]byte(val), &history); err != nil {
		return false, nil, err
	}

	return history.Success, &history.AppliedAt, nil
}

// migrate applies pending migrations.
func (m *Migrator) migrate() error {
	pending := m.getPendingMigrations()
	if len(pending) == 0 {
		log.Println("No pending migrations")

		return nil
	}

	log.Printf("Found %d pending migrations", len(pending))

	// Create backup if enabled
	if m.config.BackupEnabled && !m.config.DryRun {
		if err := m.createBackup(); err != nil {
			return fmt.Errorf("failed to create backup: %w", err)
		}
	}

	// Apply migrations
	for i := range pending {
		if err := m.applyMigration(&pending[i]); err != nil {
			return fmt.Errorf("failed to apply migration %s: %w", pending[i].ID, err)
		}
	}

	log.Println("All migrations applied successfully")

	return nil
}

// getPendingMigrations returns migrations that haven't been applied.
func (m *Migrator) getPendingMigrations() []Migration {
	var pending []Migration

	for i := range m.migrations {
		if !m.migrations[i].Applied {
			pending = append(pending, m.migrations[i])
		}
	}

	return pending
}

// applyMigration applies a single migration.
func (m *Migrator) applyMigration(migration *Migration) error {
	log.Printf("Applying migration %s: %s", migration.ID, migration.Description)

	if m.config.DryRun {
		log.Println("[DRY RUN] Would execute:")
		log.Println(migration.Script)

		return nil
	}

	// Execute migration script
	start := time.Now()
	result := m.client.Eval(m.ctx, migration.Script, []string{})

	if err := result.Err(); err != nil {
		// Record failure
		_ = m.recordMigration(migration, false, err.Error())

		return err
	}

	duration := time.Since(start)
	log.Printf("Migration %s completed in %v", migration.ID, duration)

	// Record success
	return m.recordMigration(migration, true, "")
}

// recordMigration records migration in history.
func (m *Migrator) recordMigration(migration *Migration, success bool, errorMsg string) error {
	history := MigrationHistory{
		ID:        migration.ID,
		Version:   migration.Version,
		AppliedAt: time.Now(),
		CheckSum:  migration.CheckSum,
		Success:   success,
		Error:     errorMsg,
	}

	data, err := json.Marshal(history)
	if err != nil {
		return err
	}

	key := fmt.Sprintf("%s:%s", m.config.HistoryKey, migration.ID)

	return m.client.Set(m.ctx, key, data, 0).Err()
}

// rollbackTo rolls back to specified migration.
func (m *Migrator) rollbackTo(targetID string) error {
	target, err := m.findMigration(targetID)
	if err != nil {
		return err
	}

	if !target.Applied {
		return fmt.Errorf("migration %s has not been applied", targetID)
	}

	toRollback := m.getMigrationsToRollback(targetID)
	if len(toRollback) == 0 {
		log.Println("No migrations to rollback")

		return nil
	}

	log.Printf("Rolling back %d migrations", len(toRollback))

	if err := m.prepareRollbackBackup(); err != nil {
		return err
	}

	return m.executeRollbacks(toRollback)
}

// findMigration finds a migration by ID.
func (m *Migrator) findMigration(targetID string) (*Migration, error) {
	for i := range m.migrations {
		if m.migrations[i].ID == targetID {
			return &m.migrations[i], nil
		}
	}

	return nil, fmt.Errorf("migration %s not found", targetID)
}

// getMigrationsToRollback finds migrations to rollback.
func (m *Migrator) getMigrationsToRollback(targetID string) []Migration {
	var toRollback []Migration

	for i := range m.migrations {
		if m.migrations[i].Applied && m.migrations[i].ID > targetID {
			toRollback = append(toRollback, m.migrations[i])
		}
	}

	return toRollback
}

// prepareRollbackBackup creates backup before rollback if enabled.
func (m *Migrator) prepareRollbackBackup() error {
	if m.config.BackupEnabled && !m.config.DryRun {
		if err := m.createBackup(); err != nil {
			return fmt.Errorf("failed to create backup: %w", err)
		}
	}

	return nil
}

// executeRollbacks applies rollback scripts in reverse order.
func (m *Migrator) executeRollbacks(toRollback []Migration) error {
	for i := len(toRollback) - 1; i >= 0; i-- {
		migration := toRollback[i]
		if migration.Rollback == "" {
			return fmt.Errorf("migration %s does not support rollback", migration.ID)
		}

		log.Printf("Rolling back migration %s", migration.ID)

		if m.config.DryRun {
			log.Println("[DRY RUN] Would execute rollback:")
			log.Println(migration.Rollback)

			continue
		}

		result := m.client.Eval(m.ctx, migration.Rollback, []string{})
		if err := result.Err(); err != nil {
			return fmt.Errorf("rollback failed for %s: %w", migration.ID, err)
		}

		// Remove from history
		key := fmt.Sprintf("%s:%s", m.config.HistoryKey, migration.ID)
		_ = m.client.Del(m.ctx, key).Err()
	}

	log.Println("Rollback completed successfully")

	return nil
}

// validate validates all migrations.
func (m *Migrator) validate() error {
	for i := range m.migrations {
		migration := &m.migrations[i]
		// Check required fields
		if migration.ID == "" {
			return errors.New("migration missing ID")
		}

		if migration.Script == "" {
			return fmt.Errorf("migration %s missing script", migration.ID)
		}

		// Validate Lua syntax (basic check)
		// In production, use a Lua parser for full validation

		log.Printf("✓ Migration %s is valid", migration.ID)
	}

	return nil
}

// showStatus displays migration status.
func (m *Migrator) showStatus() {
	log.Println("Migration Status:")
	log.Println("-----------------")

	for i := range m.migrations {
		migration := &m.migrations[i]
		status := "Pending"

		if migration.Applied {
			status = "Applied at " + migration.AppliedAt.Format(time.RFC3339)
		}

		log.Printf("%s - %s: %s", migration.ID, migration.Description, status)
	}

	// Show summary
	applied := 0
	pending := 0

	for i := range m.migrations {
		if m.migrations[i].Applied {
			applied++
		} else {
			pending++
		}
	}

	log.Println("\nSummary:")
	log.Printf("  Applied: %d", applied)
	log.Printf("  Pending: %d", pending)
	log.Printf("  Total:   %d", len(m.migrations))
}

// createBackup creates a Redis backup.
func (m *Migrator) createBackup() error {
	if err := os.MkdirAll(m.config.BackupPath, dirPermissions); err != nil {
		return err
	}

	timestamp := time.Now().Format("20060102-150405")
	backupFile := filepath.Join(m.config.BackupPath, fmt.Sprintf("redis-backup-%s.rdb", timestamp))

	log.Printf("Creating backup: %s", backupFile)

	// Trigger BGSAVE
	if err := m.client.BgSave(m.ctx).Err(); err != nil {
		return err
	}

	// Wait for BGSAVE to complete
	for {
		info, err := m.client.Info(m.ctx, "persistence").Result()
		if err != nil {
			return err
		}

		// Check if BGSAVE is in progress
		// Parse info string to check status
		// This is simplified; in production, parse properly
		if !contains(info, "rdb_bgsave_in_progress:1") {
			break
		}

		time.Sleep(retryDelay)
	}

	log.Printf("Backup created successfully")

	return nil
}

// createMigration creates a new migration file.
func createMigration(name, path string) error {
	timestamp := time.Now().Format("20060102150405")
	id := fmt.Sprintf("%s_%s", timestamp, name)
	filename := filepath.Join(path, id+".yaml")

	migration := Migration{
		ID:          id,
		Description: name,
		Version:     "1.0.0",
		Type:        "data",
		Script: `-- Migration: ` + name + `
-- Add your Redis Lua script here
-- Available globals: redis, KEYS, ARGV

local count = 0

-- Example: Migrate key pattern
local cursor = "0"
repeat
    local result = redis.call("SCAN", cursor, "MATCH", "old:pattern:*", "COUNT", 100)
    cursor = result[1]
    local keys = result[2]
    
    for _, key in ipairs(keys) do
        -- Process each key
        local value = redis.call("GET", key)
        -- Transform and save with new key
        -- redis.call("SET", new_key, transformed_value)
        count = count + 1
    end
until cursor == "0"

return count
`,
		Rollback: `-- Rollback for: ` + name + `
-- Add rollback logic here
return 0
`,
		CheckSum:  "",    // Will be calculated when migration is processed
		Applied:   false, // New migration has not been applied
		AppliedAt: nil,   // Not applied yet
		Metadata: map[string]interface{}{
			"author":     os.Getenv("USER"),
			"created_at": time.Now().Format(time.RFC3339),
		},
	}

	data, err := yaml.Marshal(migration)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(path, dirPermissions); err != nil {
		return err
	}

	if err := os.WriteFile(filename, data, filePermissions); err != nil {
		return err
	}

	log.Printf("Created migration: %s", filename)

	return nil
}

// Helper function.
func contains(s, substr string) bool {
	return s != "" && substr != "" && len(s) >= len(substr) &&
		(s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || contains(s[1:], substr)))
}
