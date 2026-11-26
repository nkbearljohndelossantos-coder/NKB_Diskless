-- NKB Diskless System - Phase 2 Schema Migration
-- Version: 003_phase2_device_identity.sql
-- Adds: Device UUID, MAC inventory, RAM telemetry metrics, OS build tracking

-- 1. Device UUID for hardware-independent persistent workstation identification
ALTER TABLE clients ADD COLUMN device_uuid VARCHAR(64);

-- 2. Complete MAC Address Inventory (JSON array of all network interfaces)
ALTER TABLE clients ADD COLUMN mac_inventory TEXT;

-- 3. High-resolution memory telemetry fields (in bytes)
ALTER TABLE clients ADD COLUMN ram_total_bytes BIGINT DEFAULT 0;
ALTER TABLE clients ADD COLUMN ram_avail_bytes BIGINT DEFAULT 0;

-- 4. Operating System detailed build version
ALTER TABLE clients ADD COLUMN os_build VARCHAR(100);

-- 5. Indexes for fast device UUID lookup
CREATE UNIQUE INDEX IF NOT EXISTS idx_clients_device_uuid ON clients(device_uuid);
CREATE INDEX IF NOT EXISTS idx_clients_last_seen ON clients(last_seen);
