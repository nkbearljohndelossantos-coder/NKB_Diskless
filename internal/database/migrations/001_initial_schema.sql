-- NKB Diskless System - Initial Schema Migration
-- Version: 001_initial_schema.sql
-- Target: PostgreSQL 15+ (Compatible with SQLite3 fallback)

-- Enable UUID extension if available
CREATE TABLE IF NOT EXISTS servers (
    id VARCHAR(36) PRIMARY KEY,
    hostname VARCHAR(255) NOT NULL,
    ip_address VARCHAR(45) NOT NULL,
    role VARCHAR(50) DEFAULT 'primary', -- primary, secondary, storage_node
    system_specs TEXT, -- JSON payload of CPU, RAM, Network interfaces, Disks
    status VARCHAR(50) DEFAULT 'online', -- online, offline, degraded, maintenance
    last_heartbeat TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS virtual_disks (
    id VARCHAR(36) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    file_path VARCHAR(1024) NOT NULL,
    size_bytes BIGINT NOT NULL DEFAULT 0,
    disk_format VARCHAR(20) NOT NULL DEFAULT 'VHDX', -- VHD, VHDX, RAW, IMG
    is_golden BOOLEAN NOT NULL DEFAULT FALSE,
    is_locked BOOLEAN NOT NULL DEFAULT FALSE,
    os_type VARCHAR(100) DEFAULT 'Windows 10/11 x64',
    checksum_sha256 VARCHAR(64),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS disk_versions (
    id VARCHAR(36) PRIMARY KEY,
    disk_id VARCHAR(36) NOT NULL,
    version_number INT NOT NULL DEFAULT 1,
    version_tag VARCHAR(50) NOT NULL, -- e.g. v1.0.0, v1.1.0
    snapshot_path VARCHAR(1024) NOT NULL,
    description TEXT,
    checksum_sha256 VARCHAR(64),
    created_by VARCHAR(36),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (disk_id) REFERENCES virtual_disks(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS client_groups (
    id VARCHAR(36) PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    description TEXT,
    default_disk_id VARCHAR(36),
    boot_timeout_seconds INT DEFAULT 5,
    write_cache_policy VARCHAR(50) DEFAULT 'volatile', -- volatile (reset on boot), persistent (retain delta)
    allow_super_client BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (default_disk_id) REFERENCES virtual_disks(id) ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS clients (
    id VARCHAR(36) PRIMARY KEY,
    mac_address VARCHAR(17) UNIQUE NOT NULL, -- Standard MAC format 00:11:22:33:44:55
    ip_address VARCHAR(45),
    hostname VARCHAR(255) NOT NULL,
    group_id VARCHAR(36),
    assigned_disk_id VARCHAR(36),
    status VARCHAR(50) DEFAULT 'offline', -- online, offline, booting, maintenance
    is_super_client BOOLEAN DEFAULT FALSE,
    super_client_expires_at TIMESTAMP WITH TIME ZONE,
    boot_mode VARCHAR(20) DEFAULT 'UEFI', -- UEFI, BIOS
    write_cache_mode VARCHAR(50) DEFAULT 'volatile', -- inherited, volatile, persistent
    hardware_info TEXT, -- JSON: CPU, RAM, Motherboard, NIC model, GPU
    last_boot_time TIMESTAMP WITH TIME ZONE,
    last_seen TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (group_id) REFERENCES client_groups(id) ON DELETE SET NULL,
    FOREIGN KEY (assigned_disk_id) REFERENCES virtual_disks(id) ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS write_caches (
    id VARCHAR(36) PRIMARY KEY,
    client_id VARCHAR(36) NOT NULL,
    disk_id VARCHAR(36) NOT NULL,
    cache_path VARCHAR(1024) NOT NULL,
    allocated_bytes BIGINT NOT NULL DEFAULT 21474836480, -- 20 GB default
    used_bytes BIGINT NOT NULL DEFAULT 0,
    purge_mode VARCHAR(50) DEFAULT 'on_boot', -- on_boot, manual, scheduled
    last_flushed_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (client_id) REFERENCES clients(id) ON DELETE CASCADE,
    FOREIGN KEY (disk_id) REFERENCES virtual_disks(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS users (
    id VARCHAR(36) PRIMARY KEY,
    username VARCHAR(100) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    full_name VARCHAR(255),
    role VARCHAR(50) NOT NULL DEFAULT 'operator', -- superadmin, admin, operator, auditor
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    last_login TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS audit_logs (
    id VARCHAR(36) PRIMARY KEY,
    user_id VARCHAR(36),
    username VARCHAR(100),
    action VARCHAR(100) NOT NULL, -- CLIENT_REGISTER, DISK_ASSIGN, SUPER_CLIENT_START, etc.
    target_type VARCHAR(50) NOT NULL, -- client, disk, group, system, user
    target_id VARCHAR(36),
    details TEXT, -- JSON details or human-readable description
    ip_address VARCHAR(45),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS system_updates (
    id VARCHAR(36) PRIMARY KEY,
    version_string VARCHAR(50) NOT NULL,
    package_path VARCHAR(1024),
    checksum_sha256 VARCHAR(64) NOT NULL,
    status VARCHAR(50) NOT NULL, -- staged, applying, applied, failed, rolled_back
    migration_log TEXT,
    applied_by VARCHAR(36),
    installed_at TIMESTAMP WITH TIME ZONE,
    rollback_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS server_metrics (
    id VARCHAR(36) PRIMARY KEY,
    server_id VARCHAR(36) NOT NULL,
    cpu_usage_pct REAL,
    ram_usage_pct REAL,
    ram_used_bytes BIGINT,
    ram_total_bytes BIGINT,
    storage_read_iops REAL,
    storage_write_iops REAL,
    storage_read_bytes_sec BIGINT,
    storage_write_bytes_sec BIGINT,
    network_rx_bytes_sec BIGINT,
    network_tx_bytes_sec BIGINT,
    active_iscsi_sessions INT DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (server_id) REFERENCES servers(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS client_metrics (
    id VARCHAR(36) PRIMARY KEY,
    client_id VARCHAR(36) NOT NULL,
    cpu_usage_pct REAL,
    ram_usage_pct REAL,
    disk_read_bytes_sec BIGINT,
    disk_write_bytes_sec BIGINT,
    network_rx_bytes_sec BIGINT,
    network_tx_bytes_sec BIGINT,
    boot_duration_seconds REAL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (client_id) REFERENCES clients(id) ON DELETE CASCADE
);

-- Indexes for high-frequency lookup
CREATE INDEX IF NOT EXISTS idx_clients_mac ON clients(mac_address);
CREATE INDEX IF NOT EXISTS idx_clients_status ON clients(status);
CREATE INDEX IF NOT EXISTS idx_audit_created ON audit_logs(created_at);
CREATE INDEX IF NOT EXISTS idx_server_metrics_time ON server_metrics(created_at);
CREATE INDEX IF NOT EXISTS idx_client_metrics_time ON client_metrics(client_id, created_at);
