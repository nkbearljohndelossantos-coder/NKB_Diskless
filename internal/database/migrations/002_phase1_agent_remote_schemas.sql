-- NKB Diskless System - Phase 1 Schema Expansion
-- Version: 002_phase1_agent_remote_schemas.sql
-- Adds: Roles, Agent Credentials, Commands & Results, Disk Assignments, Maintenance Locks, System Settings

-- 1. System Settings Key-Value Store
CREATE TABLE IF NOT EXISTS system_settings (
    key VARCHAR(100) PRIMARY KEY,
    value TEXT NOT NULL,
    description TEXT,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- 2. Agent Credentials for Secure Device Authentication
CREATE TABLE IF NOT EXISTS agent_credentials (
    id VARCHAR(36) PRIMARY KEY,
    client_id VARCHAR(36) NOT NULL,
    device_token VARCHAR(255) UNIQUE NOT NULL,
    secret_hash VARCHAR(255) NOT NULL,
    status VARCHAR(50) DEFAULT 'active', -- active, revoked, expired
    last_authenticated_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (client_id) REFERENCES clients(id) ON DELETE CASCADE
);

-- 3. Workstation Remote Commands & Execution Auditing
CREATE TABLE IF NOT EXISTS commands (
    id VARCHAR(36) PRIMARY KEY,
    client_id VARCHAR(36) NOT NULL,
    issuer_user_id VARCHAR(36),
    issuer_username VARCHAR(100),
    command_type VARCHAR(50) NOT NULL, -- shutdown, restart, lock, wol, query_processes, execute_diagnostic
    payload TEXT, -- JSON parameters
    status VARCHAR(50) NOT NULL DEFAULT 'pending', -- pending, sent, executing, completed, failed, cancelled
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP WITH TIME ZONE,
    FOREIGN KEY (client_id) REFERENCES clients(id) ON DELETE CASCADE,
    FOREIGN KEY (issuer_user_id) REFERENCES users(id) ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS command_results (
    id VARCHAR(36) PRIMARY KEY,
    command_id VARCHAR(36) UNIQUE NOT NULL,
    client_id VARCHAR(36) NOT NULL,
    exit_code INT DEFAULT 0,
    output TEXT,
    error_message TEXT,
    executed_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    completed_at TIMESTAMP WITH TIME ZONE,
    FOREIGN KEY (command_id) REFERENCES commands(id) ON DELETE CASCADE,
    FOREIGN KEY (client_id) REFERENCES clients(id) ON DELETE CASCADE
);

-- 4. Disk Assignments (Local VHDX vs Network Diskless)
CREATE TABLE IF NOT EXISTS disk_assignments (
    id VARCHAR(36) PRIMARY KEY,
    client_id VARCHAR(36) NOT NULL,
    disk_id VARCHAR(36) NOT NULL,
    deployment_type VARCHAR(50) NOT NULL DEFAULT 'local_vhdx', -- local_vhdx, network_iscsi
    mount_point VARCHAR(255), -- e.g. D:\NKB_VirtualDisk
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    assigned_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (client_id) REFERENCES clients(id) ON DELETE CASCADE,
    FOREIGN KEY (disk_id) REFERENCES virtual_disks(id) ON DELETE CASCADE
);

-- 5. Exclusive Super-Client Maintenance Locks
CREATE TABLE IF NOT EXISTS maintenance_locks (
    id VARCHAR(36) PRIMARY KEY,
    disk_id VARCHAR(36) NOT NULL,
    client_id VARCHAR(36) NOT NULL,
    acquired_by VARCHAR(100) NOT NULL,
    session_token VARCHAR(255) NOT NULL,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    acquired_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
    released_at TIMESTAMP WITH TIME ZONE,
    FOREIGN KEY (disk_id) REFERENCES virtual_disks(id) ON DELETE CASCADE,
    FOREIGN KEY (client_id) REFERENCES clients(id) ON DELETE CASCADE
);

-- 6. Add Workstation Metadata Columns to Clients Table if not existing
-- (Clients table already tracks hardware_info, status, mac_address, ip_address, etc.)
CREATE INDEX IF NOT EXISTS idx_commands_client_status ON commands(client_id, status);
CREATE INDEX IF NOT EXISTS idx_agent_creds_token ON agent_credentials(device_token);
CREATE INDEX IF NOT EXISTS idx_maint_locks_disk ON maintenance_locks(disk_id, is_active);
