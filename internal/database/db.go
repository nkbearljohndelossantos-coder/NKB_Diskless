package database

import (
	crand "crypto/rand"
	"crypto/subtle"
	"database/sql"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"nkb-diskless/internal/config"
	"nkb-diskless/internal/models"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var embeddedMigrations embed.FS

// DB wraps standard *sql.DB connection pool with NKB repository operations.
type DB struct {
	*sql.DB
	driver string
}

// Connect establishes the database connection based on config, falling back to SQLite if PostgreSQL is unreachable.
func Connect(cfg *config.Config) (*DB, error) {
	var conn *sql.DB
	var err error
	driver := cfg.DBDriver

	if driver == "postgres" {
		conn, err = sql.Open("pgx", cfg.DBDSN)
		if err == nil {
			err = conn.Ping()
		}
		if err != nil {
			slog.Warn("PostgreSQL connection failed; falling back to embedded SQLite database", "err", err)
			driver = "sqlite"
		}
	}

	if driver == "sqlite" {
		dbDir := cfg.DatabaseDir
		if dbDir == "" {
			dbDir = filepath.Join(cfg.StorageRoot, "database")
		}
		_ = os.MkdirAll(dbDir, 0755)
		sqlitePath := filepath.Join(dbDir, "nkb_diskless.db")
		conn, err = sql.Open("sqlite", sqlitePath)
		if err != nil {
			return nil, fmt.Errorf("failed to open sqlite database: %w", err)
		}
		_, _ = conn.Exec("PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000;")
	}

	conn.SetMaxOpenConns(25)
	conn.SetMaxIdleConns(10)
	conn.SetConnMaxLifetime(5 * time.Minute)

	db := &DB{DB: conn, driver: driver}
	return db, nil
}

// Helper to parse time values from either postgres native time.Time or sqlite text/integers
func parseTime(val any) time.Time {
	if val == nil {
		return time.Time{}
	}
	switch v := val.(type) {
	case time.Time:
		return v
	case string:
		formats := []string{
			time.RFC3339Nano,
			time.RFC3339,
			"2006-01-02 15:04:05.999999999-07:00",
			"2006-01-02 15:04:05-07:00",
			"2006-01-02 15:04:05",
			"2006-01-02T15:04:05",
		}
		for _, f := range formats {
			if t, err := time.Parse(f, v); err == nil {
				return t
			}
		}
	}
	return time.Time{}
}

func parseNullTime(val any) *time.Time {
	if val == nil {
		return nil
	}
	t := parseTime(val)
	if t.IsZero() {
		return nil
	}
	return &t
}

// Migrate executes schema migrations in chronological order.
// If migrationsDir is empty or not found on disk, it falls back to embedded migrations.
func (db *DB) Migrate(migrationsDir string) error {
	createMigrationTable := `
	CREATE TABLE IF NOT EXISTS schema_migrations (
		version VARCHAR(255) PRIMARY KEY,
		applied_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
	);`
	if _, err := db.Exec(createMigrationTable); err != nil {
		return fmt.Errorf("failed to create schema_migrations table: %w", err)
	}

	type migrationFile struct {
		name    string
		content []byte
	}

	var migrations []migrationFile

	// Try reading from disk directory first
	useDisk := false
	if migrationsDir != "" {
		if fi, err := os.Stat(migrationsDir); err == nil && fi.IsDir() {
			entries, err := os.ReadDir(migrationsDir)
			if err == nil {
				useDisk = true
				for _, entry := range entries {
					if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
						continue
					}
					content, err := os.ReadFile(filepath.Join(migrationsDir, entry.Name()))
					if err != nil {
						return fmt.Errorf("failed to read migration %s: %w", entry.Name(), err)
					}
					migrations = append(migrations, migrationFile{
						name:    entry.Name(),
						content: content,
					})
				}
			}
		}
	}

	// Fallback to embedded migrations if disk was not used
	if !useDisk {
		entries, err := embeddedMigrations.ReadDir("migrations")
		if err != nil {
			return fmt.Errorf("cannot read embedded migrations: %w", err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
				continue
			}
			content, err := embeddedMigrations.ReadFile("migrations/" + entry.Name())
			if err != nil {
				return fmt.Errorf("failed to read embedded migration %s: %w", entry.Name(), err)
			}
			migrations = append(migrations, migrationFile{
				name:    entry.Name(),
				content: content,
			})
		}
	}

	// Sort migrations lexicographically by filename to ensure strict ordering
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].name < migrations[j].name
	})

	for _, item := range migrations {
		migrationName := item.name
		var count int
		queryCheck := "SELECT COUNT(*) FROM schema_migrations WHERE version = ?"
		if db.driver == "postgres" {
			queryCheck = "SELECT COUNT(*) FROM schema_migrations WHERE version = $1"
		}
		_ = db.QueryRow(queryCheck, migrationName).Scan(&count)

		if count > 0 {
			continue
		}

		slog.Info("Applying migration", "migration", migrationName)
		content := item.content

		tx, err := db.Begin()
		if err != nil {
			return err
		}

		queries := strings.Split(string(content), ";")
		for _, q := range queries {
			trimmed := strings.TrimSpace(q)
			if trimmed == "" {
				continue
			}
			if _, err := tx.Exec(trimmed); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("error executing migration statement in %s: %w\nQuery: %s", migrationName, err, trimmed)
			}
		}

		trackQuery := "INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)"
		if db.driver == "postgres" {
			trackQuery = "INSERT INTO schema_migrations (version, applied_at) VALUES ($1, $2)"
		}
		if _, err := tx.Exec(trackQuery, migrationName, time.Now()); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("failed to record applied migration: %w", err)
		}

		if err := tx.Commit(); err != nil {
			return err
		}
		slog.Info("Successfully applied migration", "migration", migrationName)
	}

	// Seed default administrator if users table is empty
	_ = db.EnsureDefaultAdmin()

	return nil
}

func (db *DB) rebind(query string) string {
	if db.driver != "postgres" {
		return query
	}
	parts := strings.Split(query, "?")
	if len(parts) == 1 {
		return query
	}
	var b strings.Builder
	for i := 0; i < len(parts)-1; i++ {
		b.WriteString(parts[i])
		fmt.Fprintf(&b, "$%d", i+1)
	}
	b.WriteString(parts[len(parts)-1])
	return b.String()
}

// ----------------- USERS & RBAC REPOSITORY -----------------

func (db *DB) EnsureDefaultAdmin() error {
	var count int
	_ = db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	if count == 0 {
		hash, err := bcrypt.GenerateFromPassword([]byte("nkbadmin2026"), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		now := time.Now()
		q := db.rebind(`
		INSERT INTO users (id, username, password_hash, full_name, role, is_active, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)
		_, err = db.Exec(q, uuid.New().String(), "admin", string(hash), "System Administrator", "superadmin", true, now.Format(time.RFC3339), now.Format(time.RFC3339))
		if err == nil {
			slog.Info("Initialized default administrator account", "username", "admin")
		}
		return err
	}
	return nil
}

func (db *DB) CreateUser(u *models.User) error {
	if u.ID == "" {
		u.ID = uuid.New().String()
	}
	now := time.Now()
	if u.CreatedAt.IsZero() {
		u.CreatedAt = now
	}
	if u.UpdatedAt.IsZero() {
		u.UpdatedAt = now
	}
	q := db.rebind(`
	INSERT INTO users (id, username, password_hash, full_name, role, is_active, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)
	_, err := db.Exec(q, u.ID, u.Username, u.PasswordHash, u.FullName, u.Role, u.IsActive, u.CreatedAt.Format(time.RFC3339), u.UpdatedAt.Format(time.RFC3339))
	return err
}

func (db *DB) VerifyUserPassword(username, password string) (*models.User, error) {
	q := db.rebind(`
	SELECT id, username, password_hash, full_name, role, is_active, last_login, created_at, updated_at
	FROM users WHERE username = ? AND is_active = true`)

	var u models.User
	var lastLoginRaw, createdRaw, updatedRaw any
	var fn sql.NullString

	err := db.QueryRow(q, username).Scan(
		&u.ID, &u.Username, &u.PasswordHash, &fn, &u.Role, &u.IsActive,
		&lastLoginRaw, &createdRaw, &updatedRaw,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("invalid credentials")
	}
	if err != nil {
		return nil, err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return nil, fmt.Errorf("invalid credentials")
	}

	if fn.Valid { u.FullName = fn.String }
	u.LastLogin = parseNullTime(lastLoginRaw)
	u.CreatedAt = parseTime(createdRaw)
	u.UpdatedAt = parseTime(updatedRaw)

	// Record last login
	now := time.Now()
	_, _ = db.Exec(db.rebind("UPDATE users SET last_login = ? WHERE id = ?"), now.Format(time.RFC3339), u.ID)

	return &u, nil
}

func (db *DB) GetUserByID(id string) (*models.User, error) {
	q := db.rebind(`SELECT id, username, full_name, role, is_active, created_at, updated_at FROM users WHERE id = ?`)
	var u models.User
	var fn sql.NullString
	var cr, up any
	err := db.QueryRow(q, id).Scan(&u.ID, &u.Username, &fn, &u.Role, &u.IsActive, &cr, &up)
	if err != nil {
		return nil, err
	}
	if fn.Valid { u.FullName = fn.String }
	u.CreatedAt = parseTime(cr)
	u.UpdatedAt = parseTime(up)
	return &u, nil
}

// ----------------- CLIENTS & WORKSTATIONS REPOSITORY -----------------

type rowScanner interface {
	QueryRow(query string, args ...any) *sql.Row
}

type sqlExecutor interface {
	Exec(query string, args ...any) (sql.Result, error)
}

func (db *DB) scanOneClientFrom(s rowScanner, query string, args ...any) (*models.Client, error) {
	var c models.Client
	var devUUID, macInv, ip, hw, osBuild, gid, did sql.NullString
	var superExpRaw, lastBootRaw, lastSeenRaw, createdRaw, updatedRaw any
	var grpName, diskName sql.NullString

	err := s.QueryRow(query, args...).Scan(
		&c.ID, &devUUID, &c.MACAddress, &macInv, &ip, &c.Hostname, &gid, &did,
		&c.Status, &c.IsSuperClient, &superExpRaw, &c.BootMode, &c.WriteCacheMode,
		&hw, &c.RAMTotalBytes, &c.RAMAvailBytes, &osBuild,
		&lastBootRaw, &lastSeenRaw, &createdRaw, &updatedRaw,
		&grpName, &diskName,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if devUUID.Valid { c.DeviceUUID = devUUID.String }
	if macInv.Valid { c.MACInventory = macInv.String }
	if ip.Valid { c.IPAddress = ip.String }
	if hw.Valid { c.HardwareInfo = hw.String }
	if osBuild.Valid { c.OSBuild = osBuild.String }
	if gid.Valid { c.GroupID = &gid.String }
	if did.Valid { c.AssignedDiskID = &did.String }
	c.SuperClientExpireAt = parseNullTime(superExpRaw)
	c.LastBootTime = parseNullTime(lastBootRaw)
	c.LastSeen = parseNullTime(lastSeenRaw)
	c.CreatedAt = parseTime(createdRaw)
	c.UpdatedAt = parseTime(updatedRaw)
	if grpName.Valid { c.GroupName = grpName.String }
	if diskName.Valid { c.AssignedDiskName = diskName.String }

	// Parse HardwareInfo metadata
	if c.HardwareInfo != "" {
		var hwMap map[string]any
		if err := json.Unmarshal([]byte(c.HardwareInfo), &hwMap); err == nil {
			if user, ok := hwMap["logged_in_user"].(string); ok { c.LoggedInUser = user }
			if osVer, ok := hwMap["os_version"].(string); ok { c.OSVersion = osVer }
			if upt, ok := hwMap["uptime_seconds"].(float64); ok { c.UptimeSec = int64(upt) }
			if agentSt, ok := hwMap["agent_status"].(string); ok { c.AgentStatus = agentSt }
			if agentVer, ok := hwMap["agent_version"].(string); ok { c.AgentVersion = agentVer }
			if cpuPct, ok := hwMap["cpu_usage_pct"].(float64); ok { c.CPUUsagePct = float32(cpuPct) }
			if ramPct, ok := hwMap["ram_usage_pct"].(float64); ok { c.RAMUsagePct = float32(ramPct) }
		}
	}

	return &c, nil
}

func (db *DB) scanOneClient(query string, args ...any) (*models.Client, error) {
	return db.scanOneClientFrom(db.DB, query, args...)
}

func (db *DB) ListClients() ([]models.Client, error) {
	q := `
	SELECT c.id, c.device_uuid, c.mac_address, c.mac_inventory, c.ip_address, c.hostname, c.group_id, c.assigned_disk_id,
	       c.status, c.is_super_client, c.super_client_expires_at, c.boot_mode, c.write_cache_mode,
	       c.hardware_info, c.ram_total_bytes, c.ram_avail_bytes, c.os_build,
	       c.last_boot_time, c.last_seen, c.created_at, c.updated_at,
	       COALESCE(g.name, '') as group_name,
	       COALESCE(d.name, '') as assigned_disk_name
	FROM clients c
	LEFT JOIN client_groups g ON c.group_id = g.id
	LEFT JOIN virtual_disks d ON c.assigned_disk_id = d.id
	ORDER BY c.hostname ASC`

	rows, err := db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var clients []models.Client
	for rows.Next() {
		var c models.Client
		var devUUID, macInv, ip, hw, osBuild, gid, did sql.NullString
		var superExpRaw, lastBootRaw, lastSeenRaw, createdRaw, updatedRaw any
		var grpName, diskName sql.NullString

		err := rows.Scan(
			&c.ID, &devUUID, &c.MACAddress, &macInv, &ip, &c.Hostname, &gid, &did,
			&c.Status, &c.IsSuperClient, &superExpRaw, &c.BootMode, &c.WriteCacheMode,
			&hw, &c.RAMTotalBytes, &c.RAMAvailBytes, &osBuild,
			&lastBootRaw, &lastSeenRaw, &createdRaw, &updatedRaw,
			&grpName, &diskName,
		)
		if err != nil {
			return nil, err
		}

		if devUUID.Valid { c.DeviceUUID = devUUID.String }
		if macInv.Valid { c.MACInventory = macInv.String }
		if ip.Valid { c.IPAddress = ip.String }
		if hw.Valid { c.HardwareInfo = hw.String }
		if osBuild.Valid { c.OSBuild = osBuild.String }
		if gid.Valid { c.GroupID = &gid.String }
		if did.Valid { c.AssignedDiskID = &did.String }
		c.SuperClientExpireAt = parseNullTime(superExpRaw)
		c.LastBootTime = parseNullTime(lastBootRaw)
		c.LastSeen = parseNullTime(lastSeenRaw)
		c.CreatedAt = parseTime(createdRaw)
		c.UpdatedAt = parseTime(updatedRaw)
		if grpName.Valid { c.GroupName = grpName.String }
		if diskName.Valid { c.AssignedDiskName = diskName.String }

		// Parse HardwareInfo metadata
		if c.HardwareInfo != "" {
			var hwMap map[string]any
			if err := json.Unmarshal([]byte(c.HardwareInfo), &hwMap); err == nil {
				if user, ok := hwMap["logged_in_user"].(string); ok { c.LoggedInUser = user }
				if osVer, ok := hwMap["os_version"].(string); ok { c.OSVersion = osVer }
				if upt, ok := hwMap["uptime_seconds"].(float64); ok { c.UptimeSec = int64(upt) }
				if agentSt, ok := hwMap["agent_status"].(string); ok { c.AgentStatus = agentSt }
				if agentVer, ok := hwMap["agent_version"].(string); ok { c.AgentVersion = agentVer }
				if cpuPct, ok := hwMap["cpu_usage_pct"].(float64); ok { c.CPUUsagePct = float32(cpuPct) }
				if ramPct, ok := hwMap["ram_usage_pct"].(float64); ok { c.RAMUsagePct = float32(ramPct) }
			}
		}

		clients = append(clients, c)
	}

	return clients, nil
}

func (db *DB) GetClientByID(id string) (*models.Client, error) {
	q := db.rebind(`
	SELECT c.id, c.device_uuid, c.mac_address, c.mac_inventory, c.ip_address, c.hostname, c.group_id, c.assigned_disk_id,
	       c.status, c.is_super_client, c.super_client_expires_at, c.boot_mode, c.write_cache_mode,
	       c.hardware_info, c.ram_total_bytes, c.ram_avail_bytes, c.os_build,
	       c.last_boot_time, c.last_seen, c.created_at, c.updated_at,
	       COALESCE(g.name, ''), COALESCE(d.name, '')
	FROM clients c
	LEFT JOIN client_groups g ON c.group_id = g.id
	LEFT JOIN virtual_disks d ON c.assigned_disk_id = d.id
	WHERE c.id = ?`)

	return db.scanOneClient(q, id)
}

func (db *DB) getClientByDeviceUUIDFrom(s rowScanner, deviceUUID string) (*models.Client, error) {
	if deviceUUID == "" {
		return nil, nil
	}
	q := db.rebind(`
	SELECT c.id, c.device_uuid, c.mac_address, c.mac_inventory, c.ip_address, c.hostname, c.group_id, c.assigned_disk_id,
	       c.status, c.is_super_client, c.super_client_expires_at, c.boot_mode, c.write_cache_mode,
	       c.hardware_info, c.ram_total_bytes, c.ram_avail_bytes, c.os_build,
	       c.last_boot_time, c.last_seen, c.created_at, c.updated_at,
	       COALESCE(g.name, ''), COALESCE(d.name, '')
	FROM clients c
	LEFT JOIN client_groups g ON c.group_id = g.id
	LEFT JOIN virtual_disks d ON c.assigned_disk_id = d.id
	WHERE c.device_uuid = ?`)

	return db.scanOneClientFrom(s, q, deviceUUID)
}

func (db *DB) GetClientByDeviceUUID(deviceUUID string) (*models.Client, error) {
	return db.getClientByDeviceUUIDFrom(db.DB, deviceUUID)
}

func (db *DB) getClientByMACFrom(s rowScanner, mac string) (*models.Client, error) {
	macNorm := strings.ToUpper(strings.ReplaceAll(mac, "-", ":"))
	if macNorm == "" {
		return nil, nil
	}
	q := db.rebind(`
	SELECT c.id, c.device_uuid, c.mac_address, c.mac_inventory, c.ip_address, c.hostname, c.group_id, c.assigned_disk_id,
	       c.status, c.is_super_client, c.super_client_expires_at, c.boot_mode, c.write_cache_mode,
	       c.hardware_info, c.ram_total_bytes, c.ram_avail_bytes, c.os_build,
	       c.last_boot_time, c.last_seen, c.created_at, c.updated_at,
	       COALESCE(g.name, ''), COALESCE(d.name, '')
	FROM clients c
	LEFT JOIN client_groups g ON c.group_id = g.id
	LEFT JOIN virtual_disks d ON c.assigned_disk_id = d.id
	WHERE UPPER(c.mac_address) = ?`)

	return db.scanOneClientFrom(s, q, macNorm)
}

func (db *DB) GetClientByMAC(mac string) (*models.Client, error) {
	return db.getClientByMACFrom(db.DB, mac)
}

func (db *DB) createClientWith(ex sqlExecutor, c *models.Client) error {
	if c.ID == "" {
		c.ID = uuid.New().String()
	}
	if c.DeviceUUID == "" {
		c.DeviceUUID = uuid.New().String()
	}
	c.MACAddress = strings.ToUpper(strings.ReplaceAll(c.MACAddress, "-", ":"))
	now := time.Now()
	c.CreatedAt = now
	c.UpdatedAt = now

	q := db.rebind(`
	INSERT INTO clients (
		id, device_uuid, mac_address, mac_inventory, ip_address, hostname, group_id, assigned_disk_id,
		status, is_super_client, boot_mode, write_cache_mode, hardware_info,
		ram_total_bytes, ram_avail_bytes, os_build,
		created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)

	_, err := ex.Exec(q,
		c.ID, c.DeviceUUID, c.MACAddress, c.MACInventory, c.IPAddress, c.Hostname, c.GroupID, c.AssignedDiskID,
		c.Status, c.IsSuperClient, c.BootMode, c.WriteCacheMode, c.HardwareInfo,
		c.RAMTotalBytes, c.RAMAvailBytes, c.OSBuild,
		c.CreatedAt.Format(time.RFC3339), c.UpdatedAt.Format(time.RFC3339),
	)
	return err
}

func (db *DB) CreateClient(c *models.Client) error {
	return db.createClientWith(db.DB, c)
}

func (db *DB) updateClientWith(ex sqlExecutor, c *models.Client) error {
	c.UpdatedAt = time.Now()
	c.MACAddress = strings.ToUpper(strings.ReplaceAll(c.MACAddress, "-", ":"))

	q := db.rebind(`
	UPDATE clients SET
		device_uuid = COALESCE(NULLIF(?, ''), device_uuid),
		mac_address = ?,
		mac_inventory = COALESCE(NULLIF(?, ''), mac_inventory),
		ip_address = ?,
		hostname = ?,
		group_id = ?,
		assigned_disk_id = ?,
		status = ?,
		boot_mode = ?,
		write_cache_mode = ?,
		hardware_info = ?,
		ram_total_bytes = ?,
		ram_avail_bytes = ?,
		os_build = COALESCE(NULLIF(?, ''), os_build),
		last_seen = COALESCE(?, last_seen),
		updated_at = ?
	WHERE id = ?`)

	var lastSeenStr *string
	if c.LastSeen != nil {
		ls := c.LastSeen.Format(time.RFC3339)
		lastSeenStr = &ls
	}

	_, err := ex.Exec(q,
		c.DeviceUUID, c.MACAddress, c.MACInventory, c.IPAddress, c.Hostname, c.GroupID,
		c.AssignedDiskID, c.Status, c.BootMode, c.WriteCacheMode,
		c.HardwareInfo, c.RAMTotalBytes, c.RAMAvailBytes, c.OSBuild,
		lastSeenStr, c.UpdatedAt.Format(time.RFC3339), c.ID,
	)
	return err
}

func (db *DB) UpdateClient(c *models.Client) error {
	return db.updateClientWith(db.DB, c)
}

func (db *DB) DeleteClient(id string) error {
	q := db.rebind(`DELETE FROM clients WHERE id = ?`)
	_, err := db.Exec(q, id)
	return err
}

func (db *DB) SetSuperClient(id string, enable bool, expireMinutes int) error {
	now := time.Now()
	var expireAt *string
	if enable && expireMinutes > 0 {
		expStr := now.Add(time.Duration(expireMinutes) * time.Minute).Format(time.RFC3339)
		expireAt = &expStr
	}

	q := db.rebind(`
	UPDATE clients SET
		is_super_client = ?, super_client_expires_at = ?, updated_at = ?
	WHERE id = ?`)

	_, err := db.Exec(q, enable, expireAt, now.Format(time.RFC3339), id)
	return err
}

func (db *DB) RecordHeartbeat(mac string, ip string, hardwareSpecs string) error {
	now := time.Now()
	macNorm := strings.ToUpper(strings.ReplaceAll(mac, "-", ":"))

	q := db.rebind(`
	UPDATE clients SET
		last_seen = ?, ip_address = COALESCE(NULLIF(?, ''), ip_address),
		hardware_info = COALESCE(NULLIF(?, ''), hardware_info),
		status = 'online', updated_at = ?
	WHERE UPPER(mac_address) = ?`)

	res, err := db.Exec(q, now.Format(time.RFC3339), ip, hardwareSpecs, now.Format(time.RFC3339), macNorm)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		autoClient := &models.Client{
			ID:             uuid.New().String(),
			MACAddress:     macNorm,
			IPAddress:      ip,
			Hostname:       fmt.Sprintf("NKB-AUTO-%s", strings.ReplaceAll(macNorm, ":", "")[6:]),
			Status:         "discovered",
			BootMode:       "UEFI",
			WriteCacheMode: "volatile",
			HardwareInfo:   hardwareSpecs,
		}
		return db.CreateClient(autoClient)
	}
	return nil
}

// ----------------- AGENT REGISTRATION & HEARTBEAT -----------------

func (db *DB) RegisterAgent(req *models.AgentRegisterRequest) (*models.AgentRegisterResponse, error) {
	macNorm := strings.ToUpper(strings.ReplaceAll(req.MACAddress, "-", ":"))

	// Begin atomic multi-table registration transaction
	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin registration transaction: %w", err)
	}
	defer tx.Rollback()

	var client *models.Client

	// 1. Match by DeviceUUID if provided
	if req.DeviceUUID != "" {
		client, err = db.getClientByDeviceUUIDFrom(tx, req.DeviceUUID)
		if err != nil {
			return nil, err
		}
	}

	// 2. Fallback to Primary MAC
	if client == nil && macNorm != "" {
		client, err = db.getClientByMACFrom(tx, macNorm)
		if err != nil {
			return nil, err
		}
	}

	// 3. Fallback to any inventory MAC
	if client == nil && len(req.MACInventory) > 0 {
		for _, altMAC := range req.MACInventory {
			altNorm := strings.ToUpper(strings.ReplaceAll(altMAC, "-", ":"))
			if altNorm == "" || altNorm == "00:00:00:00:00:00" {
				continue
			}
			client, err = db.getClientByMACFrom(tx, altNorm)
			if err != nil {
				return nil, err
			}
			if client != nil {
				break
			}
		}
	}

	now := time.Now()
	var clientID string

	// Prepare inventory JSON
	macInvJSON := ""
	if len(req.MACInventory) > 0 {
		if b, err := json.Marshal(req.MACInventory); err == nil {
			macInvJSON = string(b)
		}
	}

	if client == nil {
		clientID = uuid.New().String()
		devUUID := req.DeviceUUID
		if devUUID == "" {
			devUUID = uuid.New().String()
		}
		client = &models.Client{
			ID:             clientID,
			DeviceUUID:     devUUID,
			MACAddress:     macNorm,
			MACInventory:   macInvJSON,
			IPAddress:      req.IPAddress,
			Hostname:       req.Hostname,
			Status:         "online",
			BootMode:       "UEFI",
			WriteCacheMode: "volatile",
			HardwareInfo:   req.HardwareInfo,
			RAMTotalBytes:  req.TotalRAMBytes,
			OSBuild:        req.OSBuild,
			LastSeen:       &now,
		}
		if err := db.createClientWith(tx, client); err != nil {
			return nil, err
		}

		// Generate cryptographically secure device token (256 bits entropy)
		tokenBytes := make([]byte, 32)
		if _, err := crand.Read(tokenBytes); err != nil {
			return nil, fmt.Errorf("cryptographic entropy failure: %w", err)
		}
		deviceToken := fmt.Sprintf("nkb_agt_%s", hex.EncodeToString(tokenBytes))
		secretHash, _ := bcrypt.GenerateFromPassword([]byte(deviceToken), bcrypt.DefaultCost)

		credID := uuid.New().String()
		qCred := db.rebind(`
		INSERT INTO agent_credentials (id, client_id, device_token, secret_hash, status, last_authenticated_at, created_at)
		VALUES (?, ?, ?, ?, 'active', ?, ?)`)
		_, err = tx.Exec(qCred, credID, clientID, deviceToken, string(secretHash), now.Format(time.RFC3339), now.Format(time.RFC3339))
		if err != nil {
			return nil, fmt.Errorf("failed to store agent credentials: %w", err)
		}

		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("failed to commit registration: %w", err)
		}

		return &models.AgentRegisterResponse{
			ClientID:    clientID,
			DeviceToken: deviceToken,
			Status:      "registered",
		}, nil
	}

	// Existing Client: Protect against unauthorized token extraction / hijacking
	clientID = client.ID
	var activeToken string
	qFindCred := db.rebind(`SELECT device_token FROM agent_credentials WHERE client_id = ? AND status = 'active'`)
	err = tx.QueryRow(qFindCred, clientID).Scan(&activeToken)
	hasActiveCred := (err == nil && activeToken != "")

	if hasActiveCred {
		// Existing registered device must prove identity with valid token using constant-time comparison
		tokenValid := req.DeviceToken != "" && subtle.ConstantTimeCompare([]byte(req.DeviceToken), []byte(activeToken)) == 1
		if !tokenValid {
			return nil, fmt.Errorf("unauthorized: client '%s' is already registered; existing valid device token required to update registration", client.ID)
		}
		// Validated: update timestamp
		qUpd := db.rebind(`UPDATE agent_credentials SET last_authenticated_at = ? WHERE device_token = ?`)
		_, _ = tx.Exec(qUpd, now.Format(time.RFC3339), activeToken)
	} else {
		// Reissue secure token if previous was revoked or missing
		tokenBytes := make([]byte, 32)
		if _, err := crand.Read(tokenBytes); err != nil {
			return nil, fmt.Errorf("cryptographic entropy failure: %w", err)
		}
		activeToken = fmt.Sprintf("nkb_agt_%s", hex.EncodeToString(tokenBytes))
		secretHash, _ := bcrypt.GenerateFromPassword([]byte(activeToken), bcrypt.DefaultCost)

		credID := uuid.New().String()
		qCred := db.rebind(`
		INSERT INTO agent_credentials (id, client_id, device_token, secret_hash, status, last_authenticated_at, created_at)
		VALUES (?, ?, ?, ?, 'active', ?, ?)`)
		_, err = tx.Exec(qCred, credID, clientID, activeToken, string(secretHash), now.Format(time.RFC3339), now.Format(time.RFC3339))
		if err != nil {
			qUpd := db.rebind(`UPDATE agent_credentials SET device_token = ?, secret_hash = ?, status = 'active', last_authenticated_at = ? WHERE client_id = ?`)
			_, _ = tx.Exec(qUpd, activeToken, string(secretHash), now.Format(time.RFC3339), clientID)
		}
	}

	if req.DeviceUUID != "" {
		client.DeviceUUID = req.DeviceUUID
	}
	if macNorm != "" {
		client.MACAddress = macNorm
	}
	if macInvJSON != "" {
		client.MACInventory = macInvJSON
	}
	client.Hostname = req.Hostname
	client.IPAddress = req.IPAddress
	client.Status = "online"
	if req.HardwareInfo != "" {
		client.HardwareInfo = req.HardwareInfo
	}
	if req.TotalRAMBytes > 0 {
		client.RAMTotalBytes = req.TotalRAMBytes
	}
	if req.OSBuild != "" {
		client.OSBuild = req.OSBuild
	}
	client.LastSeen = &now
	if err := db.updateClientWith(tx, client); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit registration update: %w", err)
	}

	return &models.AgentRegisterResponse{
		ClientID:    clientID,
		DeviceToken: activeToken,
		Status:      "registered",
	}, nil
}

func (db *DB) RevokeAgentToken(clientID string) error {
	now := time.Now()
	q := db.rebind(`UPDATE agent_credentials SET status = 'revoked', last_authenticated_at = ? WHERE client_id = ?`)
	_, err := db.Exec(q, now.Format(time.RFC3339), clientID)
	return err
}

func (db *DB) RevokeDeviceToken(token string) error {
	now := time.Now()
	q := db.rebind(`UPDATE agent_credentials SET status = 'revoked', last_authenticated_at = ? WHERE device_token = ?`)
	_, err := db.Exec(q, now.Format(time.RFC3339), token)
	return err
}

func (db *DB) AuthenticateAgent(deviceToken string) (*models.Client, error) {
	if strings.TrimSpace(deviceToken) == "" {
		return nil, fmt.Errorf("device token cannot be empty")
	}

	// 1. Fetch active credential by token
	var clientID, storedToken string
	qCred := db.rebind(`SELECT client_id, device_token FROM agent_credentials WHERE device_token = ? AND status = 'active'`)
	err := db.QueryRow(qCred, deviceToken).Scan(&clientID, &storedToken)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("invalid or revoked device token")
	}
	if err != nil {
		return nil, err
	}

	// 2. Constant-time comparison to prevent side-channel timing leaks
	if subtle.ConstantTimeCompare([]byte(deviceToken), []byte(storedToken)) != 1 {
		return nil, fmt.Errorf("invalid or revoked device token")
	}

	// 3. Load full client profile
	client, err := db.GetClientByID(clientID)
	if err != nil {
		return nil, err
	}
	if client == nil {
		return nil, fmt.Errorf("client not found for authenticated token")
	}

	// 4. Update last_authenticated_at timestamp
	now := time.Now()
	qUpd := db.rebind(`UPDATE agent_credentials SET last_authenticated_at = ? WHERE device_token = ?`)
	_, _ = db.Exec(qUpd, now.Format(time.RFC3339), storedToken)

	return client, nil
}

func (db *DB) ProcessAgentHeartbeat(clientID string, req *models.AgentHeartbeatRequest) error {
	now := time.Now()

	// Build enriched HardwareInfo JSON payload
	hwData := map[string]any{
		"logged_in_user":  req.LoggedInUser,
		"uptime_seconds":  req.UptimeSec,
		"cpu_usage_pct":   req.CPUUsagePct,
		"ram_usage_pct":   req.RAMUsagePct,
		"ram_total_bytes": req.RAMTotalBytes,
		"ram_avail_bytes": req.RAMAvailBytes,
		"disk_usage_pct":  req.DiskUsagePct,
		"network_rx_mbs":  req.NetworkRxMBs,
		"network_tx_mbs":  req.NetworkTxMBs,
		"agent_status":    req.AgentStatus,
	}
	hwBytes, _ := json.Marshal(hwData)

	q := db.rebind(`
	UPDATE clients SET
		status = 'online',
		last_seen = ?,
		hardware_info = ?,
		ram_total_bytes = CASE WHEN ? > 0 THEN ? ELSE ram_total_bytes END,
		ram_avail_bytes = CASE WHEN ? > 0 THEN ? ELSE ram_avail_bytes END,
		updated_at = ?
	WHERE id = ?`)

	_, err := db.Exec(q, now.Format(time.RFC3339), string(hwBytes), req.RAMTotalBytes, req.RAMTotalBytes, req.RAMAvailBytes, req.RAMAvailBytes, now.Format(time.RFC3339), clientID)
	return err
}

func (db *DB) MarkOfflineStaleClients(timeout time.Duration) (int64, error) {
	cutoff := time.Now().Add(-timeout)
	now := time.Now()
	q := db.rebind(`
	UPDATE clients SET status = 'offline', updated_at = ?
	WHERE status = 'online' AND last_seen < ?`)

	res, err := db.Exec(q, now.Format(time.RFC3339), cutoff.Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ----------------- REMOTE COMMANDS REPOSITORY -----------------

func (db *DB) QueueCommand(cmd *models.RemoteCommand) error {
	if cmd.ID == "" {
		cmd.ID = uuid.New().String()
	}
	now := time.Now()
	cmd.CreatedAt = now
	cmd.Status = "pending"

	var expStr *string
	if cmd.ExpiresAt != nil {
		es := cmd.ExpiresAt.Format(time.RFC3339)
		expStr = &es
	}

	q := db.rebind(`
	INSERT INTO commands (id, client_id, issuer_user_id, issuer_username, command_type, payload, status, created_at, expires_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)

	_, err := db.Exec(q, cmd.ID, cmd.ClientID, cmd.IssuerUserID, cmd.IssuerUsername, cmd.CommandType, cmd.Payload, cmd.Status, now.Format(time.RFC3339), expStr)
	return err
}

func (db *DB) GetPendingCommands(clientID string) ([]models.RemoteCommand, error) {
	q := db.rebind(`
	SELECT id, client_id, issuer_user_id, issuer_username, command_type, payload, status, created_at
	FROM commands WHERE client_id = ? AND status = 'pending' ORDER BY created_at ASC`)

	rows, err := db.Query(q, clientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cmds []models.RemoteCommand
	for rows.Next() {
		var c models.RemoteCommand
		var uid, un, pl sql.NullString
		var cr any
		err := rows.Scan(&c.ID, &c.ClientID, &uid, &un, &c.CommandType, &pl, &c.Status, &cr)
		if err != nil {
			return nil, err
		}
		if uid.Valid { c.IssuerUserID = &uid.String }
		if un.Valid { c.IssuerUsername = un.String }
		if pl.Valid { c.Payload = pl.String }
		c.CreatedAt = parseTime(cr)
		cmds = append(cmds, c)
	}

	// Mark queried commands as sent
	if len(cmds) > 0 {
		_, _ = db.Exec(db.rebind(`UPDATE commands SET status = 'sent' WHERE client_id = ? AND status = 'pending'`), clientID)
	}

	return cmds, nil
}

func (db *DB) RecordCommandResult(res *models.CommandResult) error {
	if res.ID == "" {
		res.ID = uuid.New().String()
	}
	now := time.Now()
	res.CompletedAt = &now

	q := db.rebind(`
	INSERT INTO command_results (id, command_id, client_id, exit_code, output, error_message, executed_at, completed_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)

	_, err := db.Exec(q, res.ID, res.CommandID, res.ClientID, res.ExitCode, res.Output, res.ErrorMessage, res.ExecutedAt.Format(time.RFC3339), now.Format(time.RFC3339))
	if err != nil {
		// Idempotent retry: update existing result record if command_id was already saved
		qUpd := db.rebind(`
		UPDATE command_results SET
			exit_code = ?, output = ?, error_message = ?, completed_at = ?
		WHERE command_id = ?`)
		if _, updErr := db.Exec(qUpd, res.ExitCode, res.Output, res.ErrorMessage, now.Format(time.RFC3339), res.CommandID); updErr != nil {
			return err
		}
	}

	status := "completed"
	if res.ExitCode != 0 {
		status = "failed"
	}
	_, _ = db.Exec(db.rebind(`UPDATE commands SET status = ? WHERE id = ?`), status, res.CommandID)
	return nil
}

func (db *DB) ListCommands(clientID string, limit int) ([]models.RemoteCommand, error) {
	if limit <= 0 {
		limit = 20
	}
	q := db.rebind(`
	SELECT c.id, c.client_id, c.issuer_username, c.command_type, c.payload, c.status, c.created_at,
	       cr.exit_code, cr.output, cr.error_message
	FROM commands c
	LEFT JOIN command_results cr ON c.id = cr.command_id
	WHERE c.client_id = ?
	ORDER BY c.created_at DESC LIMIT ?`)

	rows, err := db.Query(q, clientID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cmds []models.RemoteCommand
	for rows.Next() {
		var c models.RemoteCommand
		var un, pl sql.NullString
		var cr any
		var exitCode sql.NullInt32
		var output, errMsg sql.NullString

		err := rows.Scan(&c.ID, &c.ClientID, &un, &c.CommandType, &pl, &c.Status, &cr, &exitCode, &output, &errMsg)
		if err != nil {
			return nil, err
		}
		if un.Valid { c.IssuerUsername = un.String }
		if pl.Valid { c.Payload = pl.String }
		c.CreatedAt = parseTime(cr)

		if exitCode.Valid {
			c.Result = &models.CommandResult{
				CommandID:    c.ID,
				ClientID:     c.ClientID,
				ExitCode:     int(exitCode.Int32),
				Output:       output.String,
				ErrorMessage: errMsg.String,
			}
		}

		cmds = append(cmds, c)
	}
	return cmds, nil
}

// ----------------- VIRTUAL DISKS REPOSITORY -----------------

func (db *DB) ListDisks() ([]models.VirtualDisk, error) {
	q := `
	SELECT id, name, description, file_path, size_bytes, disk_format,
	       is_golden, is_locked, os_type, checksum_sha256, created_at, updated_at
	FROM virtual_disks ORDER BY name ASC`

	rows, err := db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var disks []models.VirtualDisk
	for rows.Next() {
		var d models.VirtualDisk
		var desc, chk, fmtStr, osTypeStr sql.NullString
		var sizeVal sql.NullInt64
		var goldenVal, lockedVal sql.NullBool
		var createdRaw, updatedRaw any
		err := rows.Scan(
			&d.ID, &d.Name, &desc, &d.FilePath, &sizeVal, &fmtStr,
			&goldenVal, &lockedVal, &osTypeStr, &chk, &createdRaw, &updatedRaw,
		)
		if err != nil {
			return nil, err
		}
		if desc.Valid { d.Description = desc.String }
		if chk.Valid { d.ChecksumSHA256 = chk.String }
		if fmtStr.Valid { d.DiskFormat = fmtStr.String } else { d.DiskFormat = "VHDX" }
		if osTypeStr.Valid { d.OSType = osTypeStr.String }
		if sizeVal.Valid { d.SizeBytes = sizeVal.Int64 }
		if goldenVal.Valid { d.IsGolden = goldenVal.Bool }
		if lockedVal.Valid { d.IsLocked = lockedVal.Bool }
		d.CreatedAt = parseTime(createdRaw)
		d.UpdatedAt = parseTime(updatedRaw)
		disks = append(disks, d)
	}
	return disks, nil
}

func (db *DB) GetDiskByID(id string) (*models.VirtualDisk, error) {
	q := db.rebind(`
	SELECT id, name, description, file_path, size_bytes, disk_format,
	       is_golden, is_locked, os_type, checksum_sha256, created_at, updated_at
	FROM virtual_disks WHERE id = ?`)

	var d models.VirtualDisk
	var desc, chk, fmtStr, osTypeStr sql.NullString
	var sizeVal sql.NullInt64
	var goldenVal, lockedVal sql.NullBool
	var createdRaw, updatedRaw any
	err := db.QueryRow(q, id).Scan(
		&d.ID, &d.Name, &desc, &d.FilePath, &sizeVal, &fmtStr,
		&goldenVal, &lockedVal, &osTypeStr, &chk, &createdRaw, &updatedRaw,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if desc.Valid { d.Description = desc.String }
	if chk.Valid { d.ChecksumSHA256 = chk.String }
	if fmtStr.Valid { d.DiskFormat = fmtStr.String } else { d.DiskFormat = "VHDX" }
	if osTypeStr.Valid { d.OSType = osTypeStr.String }
	if sizeVal.Valid { d.SizeBytes = sizeVal.Int64 }
	if goldenVal.Valid { d.IsGolden = goldenVal.Bool }
	if lockedVal.Valid { d.IsLocked = lockedVal.Bool }
	d.CreatedAt = parseTime(createdRaw)
	d.UpdatedAt = parseTime(updatedRaw)
	return &d, nil
}

func (db *DB) CreateDisk(d *models.VirtualDisk) error {
	if d.ID == "" {
		d.ID = uuid.New().String()
	}
	now := time.Now()
	d.CreatedAt = now
	d.UpdatedAt = now

	q := db.rebind(`
	INSERT INTO virtual_disks (
		id, name, description, file_path, size_bytes, disk_format,
		is_golden, is_locked, os_type, checksum_sha256, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)

	_, err := db.Exec(q,
		d.ID, d.Name, d.Description, d.FilePath, d.SizeBytes, d.DiskFormat,
		d.IsGolden, d.IsLocked, d.OSType, d.ChecksumSHA256,
		d.CreatedAt.Format(time.RFC3339), d.UpdatedAt.Format(time.RFC3339),
	)
	return err
}

func (db *DB) UpdateDisk(d *models.VirtualDisk) error {
	d.UpdatedAt = time.Now()
	q := db.rebind(`
	UPDATE virtual_disks SET
		name = ?, description = ?, file_path = ?, size_bytes = ?,
		disk_format = ?, is_golden = ?, is_locked = ?, os_type = ?,
		checksum_sha256 = ?, updated_at = ?
	WHERE id = ?`)

	_, err := db.Exec(q,
		d.Name, d.Description, d.FilePath, d.SizeBytes,
		d.DiskFormat, d.IsGolden, d.IsLocked, d.OSType,
		d.ChecksumSHA256, d.UpdatedAt.Format(time.RFC3339), d.ID,
	)
	return err
}

func (db *DB) DeleteDisk(id string) error {
	var isLocked bool
	_ = db.QueryRow(db.rebind("SELECT is_locked FROM virtual_disks WHERE id = ?"), id).Scan(&isLocked)
	if isLocked {
		return fmt.Errorf("cannot delete disk %s: disk is marked as locked/protected", id)
	}

	q := db.rebind(`DELETE FROM virtual_disks WHERE id = ?`)
	_, err := db.Exec(q, id)
	return err
}

// ----------------- CLIENT GROUPS REPOSITORY -----------------

func (db *DB) ListGroups() ([]models.ClientGroup, error) {
	q := `
	SELECT g.id, g.name, g.description, g.default_disk_id, g.boot_timeout_seconds,
	       g.write_cache_policy, g.allow_super_client, g.created_at, g.updated_at,
	       COUNT(c.id) as client_count
	FROM client_groups g
	LEFT JOIN clients c ON c.group_id = g.id
	GROUP BY g.id, g.name, g.description, g.default_disk_id, g.boot_timeout_seconds,
	         g.write_cache_policy, g.allow_super_client, g.created_at, g.updated_at
	ORDER BY g.name ASC`

	rows, err := db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []models.ClientGroup
	for rows.Next() {
		var g models.ClientGroup
		var desc, defDisk sql.NullString
		var createdRaw, updatedRaw any
		err := rows.Scan(
			&g.ID, &g.Name, &desc, &defDisk, &g.BootTimeoutSeconds,
			&g.WriteCachePolicy, &g.AllowSuperClient, &createdRaw, &updatedRaw,
			&g.ClientCount,
		)
		if err != nil {
			return nil, err
		}
		if desc.Valid { g.Description = desc.String }
		if defDisk.Valid { g.DefaultDiskID = &defDisk.String }
		g.CreatedAt = parseTime(createdRaw)
		g.UpdatedAt = parseTime(updatedRaw)
		groups = append(groups, g)
	}
	return groups, nil
}

func (db *DB) CreateGroup(g *models.ClientGroup) error {
	if g.ID == "" {
		g.ID = uuid.New().String()
	}
	now := time.Now()
	g.CreatedAt = now
	g.UpdatedAt = now

	q := db.rebind(`
	INSERT INTO client_groups (
		id, name, description, default_disk_id, boot_timeout_seconds,
		write_cache_policy, allow_super_client, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)

	_, err := db.Exec(q,
		g.ID, g.Name, g.Description, g.DefaultDiskID, g.BootTimeoutSeconds,
		g.WriteCachePolicy, g.AllowSuperClient,
		g.CreatedAt.Format(time.RFC3339), g.UpdatedAt.Format(time.RFC3339),
	)
	return err
}

// ----------------- AUDIT LOGS REPOSITORY -----------------

func (db *DB) RecordAudit(userID *string, username string, action string, targetType string, targetID string, details string, ip string) error {
	id := uuid.New().String()
	now := time.Now()
	q := db.rebind(`
	INSERT INTO audit_logs (id, user_id, username, action, target_type, target_id, details, ip_address, created_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)

	_, err := db.Exec(q, id, userID, username, action, targetType, targetID, details, ip, now.Format(time.RFC3339))
	return err
}

func (db *DB) ListAuditLogs(limit int) ([]models.AuditLog, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	q := db.rebind(`
	SELECT id, user_id, username, action, target_type, target_id, details, ip_address, created_at
	FROM audit_logs ORDER BY created_at DESC LIMIT ?`)

	rows, err := db.Query(q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []models.AuditLog
	for rows.Next() {
		var l models.AuditLog
		var uid, user, det, ip sql.NullString
		var createdRaw any
		err := rows.Scan(
			&l.ID, &uid, &user, &l.Action, &l.TargetType, &l.TargetID, &det, &ip, &createdRaw,
		)
		if err != nil {
			return nil, err
		}
		if uid.Valid { l.UserID = &uid.String }
		if user.Valid { l.Username = user.String }
		if det.Valid { l.Details = det.String }
		if ip.Valid { l.IPAddress = ip.String }
		l.CreatedAt = parseTime(createdRaw)
		logs = append(logs, l)
	}
	return logs, nil
}

// ----------------- DISK VERSIONS, ASSIGNMENTS & MAINTENANCE LOCKS -----------------

func (db *DB) CreateDiskVersion(v *models.DiskVersion) error {
	if v.ID == "" {
		v.ID = uuid.New().String()
	}
	if v.CreatedAt.IsZero() {
		v.CreatedAt = time.Now()
	}

	q := db.rebind(`
	INSERT INTO disk_versions (
		id, disk_id, version_number, version_tag, snapshot_path, description,
		checksum_sha256, created_by, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)

	_, err := db.Exec(q,
		v.ID, v.DiskID, v.VersionNumber, v.VersionTag, v.SnapshotPath, v.Description,
		v.ChecksumSHA256, v.CreatedBy, v.CreatedAt.Format(time.RFC3339),
	)
	return err
}

func (db *DB) ListDiskVersions(diskID string) ([]models.DiskVersion, error) {
	q := db.rebind(`
	SELECT id, disk_id, version_number, version_tag, snapshot_path, description,
	       checksum_sha256, created_by, created_at
	FROM disk_versions WHERE disk_id = ? ORDER BY version_number DESC`)

	rows, err := db.Query(q, diskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var versions []models.DiskVersion
	for rows.Next() {
		var v models.DiskVersion
		var desc, chk, createdBy sql.NullString
		var createdRaw any
		err := rows.Scan(
			&v.ID, &v.DiskID, &v.VersionNumber, &v.VersionTag, &v.SnapshotPath,
			&desc, &chk, &createdBy, &createdRaw,
		)
		if err != nil {
			return nil, err
		}
		if desc.Valid { v.Description = desc.String }
		if chk.Valid { v.ChecksumSHA256 = chk.String }
		if createdBy.Valid { v.CreatedBy = &createdBy.String }
		v.CreatedAt = parseTime(createdRaw)
		versions = append(versions, v)
	}
	return versions, nil
}

func (db *DB) GetDiskVersionByID(id string) (*models.DiskVersion, error) {
	q := db.rebind(`
	SELECT id, disk_id, version_number, version_tag, snapshot_path, description,
	       checksum_sha256, created_by, created_at
	FROM disk_versions WHERE id = ?`)

	var v models.DiskVersion
	var desc, chk, createdBy sql.NullString
	var createdRaw any
	err := db.QueryRow(q, id).Scan(
		&v.ID, &v.DiskID, &v.VersionNumber, &v.VersionTag, &v.SnapshotPath,
		&desc, &chk, &createdBy, &createdRaw,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if desc.Valid { v.Description = desc.String }
	if chk.Valid { v.ChecksumSHA256 = chk.String }
	if createdBy.Valid { v.CreatedBy = &createdBy.String }
	v.CreatedAt = parseTime(createdRaw)
	return &v, nil
}

func (db *DB) GetLatestDiskVersion(diskID string) (*models.DiskVersion, error) {
	q := db.rebind(`
	SELECT id, disk_id, version_number, version_tag, snapshot_path, description,
	       checksum_sha256, created_by, created_at
	FROM disk_versions WHERE disk_id = ? ORDER BY version_number DESC LIMIT 1`)

	var v models.DiskVersion
	var desc, chk, createdBy sql.NullString
	var createdRaw any
	err := db.QueryRow(q, diskID).Scan(
		&v.ID, &v.DiskID, &v.VersionNumber, &v.VersionTag, &v.SnapshotPath,
		&desc, &chk, &createdBy, &createdRaw,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if desc.Valid { v.Description = desc.String }
	if chk.Valid { v.ChecksumSHA256 = chk.String }
	if createdBy.Valid { v.CreatedBy = &createdBy.String }
	v.CreatedAt = parseTime(createdRaw)
	return &v, nil
}

func (db *DB) CreateDiskAssignment(a *models.DiskAssignment) error {
	if a.ID == "" {
		a.ID = uuid.New().String()
	}
	if a.AssignedAt.IsZero() {
		a.AssignedAt = time.Now()
	}

	// Deactivate any existing active assignment for this client
	deactQ := db.rebind(`UPDATE disk_assignments SET is_active = FALSE WHERE client_id = ?`)
	_, _ = db.Exec(deactQ, a.ClientID)

	q := db.rebind(`
	INSERT INTO disk_assignments (
		id, client_id, disk_id, deployment_type, mount_point, is_active, assigned_at
	) VALUES (?, ?, ?, ?, ?, ?, ?)`)

	_, err := db.Exec(q,
		a.ID, a.ClientID, a.DiskID, a.DeploymentType, a.MountPoint, a.IsActive,
		a.AssignedAt.Format(time.RFC3339),
	)
	if err != nil {
		return err
	}

	// Update clients table assigned_disk_id
	updQ := db.rebind(`UPDATE clients SET assigned_disk_id = ?, updated_at = ? WHERE id = ?`)
	_, _ = db.Exec(updQ, a.DiskID, time.Now().Format(time.RFC3339), a.ClientID)

	return nil
}

func (db *DB) GetDiskAssignment(clientID string) (*models.DiskAssignment, error) {
	q := db.rebind(`
	SELECT id, client_id, disk_id, deployment_type, mount_point, is_active, assigned_at
	FROM disk_assignments WHERE client_id = ? AND is_active = TRUE LIMIT 1`)

	var a models.DiskAssignment
	var mp sql.NullString
	var assignedRaw any
	err := db.QueryRow(q, clientID).Scan(
		&a.ID, &a.ClientID, &a.DiskID, &a.DeploymentType, &mp, &a.IsActive, &assignedRaw,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if mp.Valid { a.MountPoint = mp.String }
	a.AssignedAt = parseTime(assignedRaw)
	return &a, nil
}

func (db *DB) ListDiskAssignments() ([]models.DiskAssignment, error) {
	q := `
	SELECT id, client_id, disk_id, deployment_type, mount_point, is_active, assigned_at
	FROM disk_assignments WHERE is_active = TRUE ORDER BY assigned_at DESC`

	rows, err := db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []models.DiskAssignment
	for rows.Next() {
		var a models.DiskAssignment
		var mp sql.NullString
		var assignedRaw any
		err := rows.Scan(
			&a.ID, &a.ClientID, &a.DiskID, &a.DeploymentType, &mp, &a.IsActive, &assignedRaw,
		)
		if err != nil {
			return nil, err
		}
		if mp.Valid { a.MountPoint = mp.String }
		a.AssignedAt = parseTime(assignedRaw)
		list = append(list, a)
	}
	return list, nil
}

func (db *DB) DeactivateDiskAssignment(clientID string) error {
	q := db.rebind(`UPDATE disk_assignments SET is_active = FALSE WHERE client_id = ?`)
	_, err := db.Exec(q, clientID)
	if err != nil {
		return err
	}
	updQ := db.rebind(`UPDATE clients SET assigned_disk_id = NULL, updated_at = ? WHERE id = ?`)
	_, _ = db.Exec(updQ, time.Now().Format(time.RFC3339), clientID)
	return nil
}

func (db *DB) AcquireMaintenanceLock(lock *models.MaintenanceLock) error {
	if lock.ID == "" {
		lock.ID = uuid.New().String()
	}
	if lock.AcquiredAt.IsZero() {
		lock.AcquiredAt = time.Now()
	}

	// First verify that no active lock exists for this disk that hasn't expired
	activeLock, err := db.GetActiveMaintenanceLock(lock.DiskID)
	if err != nil {
		return err
	}
	if activeLock != nil && activeLock.ExpiresAt.After(time.Now()) {
		return fmt.Errorf("disk is locked by workstation %s until %s",
			activeLock.ClientID, activeLock.ExpiresAt.Format(time.RFC3339))
	}

	// Deactivate any stale lock
	staleQ := db.rebind(`UPDATE maintenance_locks SET is_active = FALSE WHERE disk_id = ? AND is_active = TRUE`)
	_, _ = db.Exec(staleQ, lock.DiskID)

	q := db.rebind(`
	INSERT INTO maintenance_locks (
		id, disk_id, client_id, acquired_by, session_token, is_active,
		acquired_at, expires_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)

	_, err = db.Exec(q,
		lock.ID, lock.DiskID, lock.ClientID, lock.AcquiredBy, lock.SessionToken,
		lock.IsActive, lock.AcquiredAt.Format(time.RFC3339), lock.ExpiresAt.Format(time.RFC3339),
	)
	return err
}

func (db *DB) GetActiveMaintenanceLock(diskID string) (*models.MaintenanceLock, error) {
	q := db.rebind(`
	SELECT id, disk_id, client_id, acquired_by, session_token, is_active,
	       acquired_at, expires_at, released_at
	FROM maintenance_locks
	WHERE disk_id = ? AND is_active = TRUE
	ORDER BY acquired_at DESC LIMIT 1`)

	var l models.MaintenanceLock
	var acqRaw, expRaw, relRaw any
	err := db.QueryRow(q, diskID).Scan(
		&l.ID, &l.DiskID, &l.ClientID, &l.AcquiredBy, &l.SessionToken, &l.IsActive,
		&acqRaw, &expRaw, &relRaw,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	l.AcquiredAt = parseTime(acqRaw)
	l.ExpiresAt = parseTime(expRaw)
	l.ReleasedAt = parseNullTime(relRaw)

	// If expired, automatically mark inactive
	if l.ExpiresAt.Before(time.Now()) {
		_ = db.ReleaseMaintenanceLock(diskID, l.ClientID)
		return nil, nil
	}

	return &l, nil
}

func (db *DB) ReleaseMaintenanceLock(diskID string, clientID string) error {
	now := time.Now()
	q := db.rebind(`
	UPDATE maintenance_locks
	SET is_active = FALSE, released_at = ?
	WHERE disk_id = ? AND (client_id = ? OR ? = '') AND is_active = TRUE`)
	_, err := db.Exec(q, now.Format(time.RFC3339), diskID, clientID, clientID)
	return err
}
