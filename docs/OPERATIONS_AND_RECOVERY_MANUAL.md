# NKB DISKLESS SYSTEM: OPERATIONS & DISASTER RECOVERY MANUAL
**Platform:** Modular, Scalable Diskless Computing Platform for NKB Manufacturing  
**Platform Version:** 1.0.0 Enterprise  
**Author:** NKB Systems Architecture Team  

---

## 1. SYSTEM OVERVIEW & ARCHITECTURE

The **NKB Diskless System** enables client computers across manufacturing floors and offices to boot full Windows environments directly over the local network from centralized virtual disk images. Client PCs operate completely without local internal HDDs or SSDs.

### Core Capabilities
- **Zero Local Drives:** Client workstations boot Windows 10/11 over Gigabit/10GbE network interfaces.
- **Single Master Image Maintenance:** An update installed once on the server is immediately available to all 5, 20, or 100+ workstations upon reboot.
- **Immutable Golden Disks:** Production OS images are read-only; client workstations write to isolated differential write-caches.
- **Volatile Reboot Hygiene:** In volatile mode, workstations reset to a pristine state on every reboot—eliminating malware, configuration drift, and residual temp files.
- **Hardware Agnostic:** Supports standard UEFI x64 and Legacy BIOS motherboards from Intel, AMD, Realtek, etc.

---

## 2. HARDWARE SCALING & EXPANSION MATRIX

| Client Tier | Target Fleet | Minimum CPU | Server RAM | Storage Configuration | Network Interface | Write-Cache Tier |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **Proof of Concept** | 5 – 10 PCs | Intel Core i7 (6-8 Cores) | **16 GB** | 1x 500GB SSD (System + Cache) | 1x 1GbE | SSD Delta Files |
| **Small Production** | 10 – 20 PCs | 8 Cores / 16 Threads | **32 GB – 64 GB** | 1x 500GB SSD (OS)<br>1x 1TB NVMe SSD (Cache Pool) | 2x 1GbE LACP or 2.5GbE | 16GB RAM Cache + NVMe |
| **Medium Facility** | 20 – 50 PCs | 16 Cores / 32 Threads | **64 GB – 128 GB** | 2x 1TB NVMe RAID 1 (Images)<br>2x 2TB NVMe RAID 0 (High IOPS Cache) | 1x 10GbE SFP+ | 32GB RAM Cache + NVMe Array |
| **Enterprise Cluster** | 50 – 100+ PCs | Dual AMD EPYC / Xeon (32+ Cores) | **128 GB – 256 GB+** | Enterprise NVMe U.2/U.3 Array (>800k IOPS) | Dual 10GbE / 25GbE Bonded | 64GB RAM Cache + Enterprise SAN |

---

## 3. NETWORK TOPOLOGY & PORT REQUIREMENTS

Ensure the following ports are open on the NKB Server host firewall:

| Port / Protocol | Direction | Source | Service |
| :--- | :--- | :--- | :--- |
| **UDP 67 / 68** | Inbound/Outbound | Client Workstations | DHCP (IP Allocation) |
| **UDP 4011** | Inbound | Client Workstations | ProxyDHCP (PXE Boot Parameters) |
| **UDP 69** | Inbound | Client Workstations | TFTP (Stage 1 iPXE bootloader transfer) |
| **TCP 8080 / 8443** | Inbound | Client Workstations & Admins | HTTP Boot scripts & NKB REST API Dashboard |
| **TCP 3260** | Inbound | Client Workstations | iSCSI Target SAN (Block storage device) |

---

## 4. DAILY OPERATIONS & WORKFLOWS

### 4.1 Client Workstation Onboarding
1. Ensure the workstation's BIOS/UEFI settings have:
   - **Network Boot (PXE):** Enabled (set as 1st boot priority).
   - **Boot Mode:** UEFI (CSM Disabled recommended for modern Windows 11).
   - **Fast Boot:** Disabled.
2. Power on the workstation. The network card broadcasts a DHCP request.
3. The NKB Server ProxyDHCP responds and serves `ipxe.efi`.
4. The client executes iPXE and requests its dynamic configuration script.
5. If the computer is new, it is auto-registered as `discovered` in the NKB Dashboard.
6. In the **Client Workstations** tab:
   - Click the client record.
   - Assign a **Department Group** and a **Virtual Disk**.
   - Select **Write-Cache Mode** (`volatile` for factory floor, `persistent` for engineering).
7. On the next boot, the workstation boots straight to the Windows desktop!

### 4.2 Super-Client Maintenance Mode (Updating Windows / Installing Software)
To install new software, factory CAD applications, or Windows Updates across the fleet:
1. Select an authorized workstation in the **Client Workstations** tab.
2. Click **Super** to activate Super-Client Maintenance Mode.
3. Power on the designated workstation. The screen displays:
   `*** NKB SUPER-CLIENT MAINTENANCE MODE ACTIVATED ***`
4. The workstation boots against an exclusive writable snapshot.
5. Inside Windows:
   - Install required software or drivers.
   - Run Windows Update.
   - If deploying new hardware drivers, ensure they are generalized.
6. Shut down the workstation cleanly.
7. In the NKB Dashboard, click **Commit Version** and enter release notes (e.g., `v1.2 - Added Siemens PLC Suite`).
8. The server seals the new snapshot into an official version.
9. All workstations across the manufacturing floor will automatically boot with the newly installed software on their next restart!

---

## 5. NKB UPDATE MANAGER & DISASTER RECOVERY

The NKB Update Manager is engineered with zero silent destructive updates and mandatory pre-update snapshots.

### 5.1 Applying Platform Updates
1. Navigate to the **Update Manager** tab in the Dashboard.
2. Review the update version and compatibility checks.
3. Click **Apply System Update**.
4. The update engine:
   - Creates an automated atomic database backup in `backups/nkb_preupdate_<version>_<timestamp>.bak`.
   - Executes pending database migrations within a database transaction.
   - Updates core services.
5. If any step fails, the system automatically triggers a rollback to the pre-update backup point.

### 5.2 Manual Disaster Rollback
If a physical hardware failure or database corruption occurs:
1. Stop the NKB Server daemon:
   ```powershell
   Stop-Service -Name "NKBDiskless"
   ```
2. Locate the latest valid backup in the `backups/` directory:
   ```powershell
   Get-ChildItem .\backups\*.bak | Sort-Object LastWriteTime -Descending | Select-Object -First 3
   ```
3. Restore the SQLite database file:
   ```powershell
   Copy-Item .\backups\nkb_preupdate_v1.0.0_20261122.bak .\data\nkb_diskless.db -Force
   ```
4. Restart the service:
   ```powershell
   Start-Service -Name "NKBDiskless"
   ```
5. All workstation MAC registrations, group policies, and disk links are 100% restored.

---

## 6. TROUBLESHOOTING GUIDE

### Issue 1: Client displays "PXE-E51: No DHCP or ProxyDHCP offers were received"
- **Cause:** Network switch blocking UDP broadcasts or ProxyDHCP not running.
- **Remedy:**
  1. Verify NKB Server is running (`tasklist | findstr nkb-server`).
  2. Ensure UDP port 4011 and UDP port 67 are allowed through the firewall.
  3. Ensure network switch has IGMP/DHCP snooping configured to trust the NKB Server port.

### Issue 2: Client downloads `ipxe.efi` but hangs at `Connecting to NKB Virtual Block SAN...`
- **Cause:** iSCSI Target port (TCP 3260) blocked or target daemon not started.
- **Remedy:**
  1. Test port connectivity from the client network: `Test-NetConnection -ComputerName <ServerIP> -Port 3260`.
  2. Verify client MAC has an assigned disk in the NKB Dashboard.

### Issue 3: Windows boots but shows Blue Screen `INACCESSIBLE_BOOT_DEVICE` (Stop Code 0x0000007B)
- **Cause:** The Windows image does not have the network adapter driver loaded at boot, or the Microsoft iSCSI Initiator driver (`msiscsi`) is set to manual/demand start instead of boot-critical.
- **Remedy:**
  1. Run the image preparation script: `.\scripts\prepare-golden-image.ps1 -DriverDir "C:\Drivers\Realtek"`.
  2. Ensure registry key `HKLM\SYSTEM\CurrentControlSet\Services\msiscsi\Start` is set to `0` (Boot).
  3. Apply the NDIS link reset mitigation patch from the NKB Dashboard under iSCSI Manager.

---

## 7. ISOLATED TESTING VLAN 99 & SWITCH RUNBOOK

To safely test network boot without modifying or risking production corporate DHCP:

### 7.1 Managed Switch Port Configuration (Cisco / EdgeSwitch / UniFi)
```text
! Cisco IOS Isolated VLAN Configuration
vlan 99
 name NKB-Diskless-Test-VLAN
exit

! Server Interface (Port 1)
interface GigabitEthernet0/1
 description NKB-Server-Primary
 switchport mode access
 switchport access vlan 99
 spanning-tree portfast
 no shutdown
exit

! Test Client Workstation (Port 2)
interface GigabitEthernet0/2
 description NKB-Test-Client-01
 switchport mode access
 switchport access vlan 99
 spanning-tree portfast
 no shutdown
exit
```

### 7.2 Point-to-Point Direct Ethernet Cable (Zero Switch Setup)
If an isolated managed switch is not immediately available:
1. Connect a standard Cat5e/Cat6 patch cable directly between the server's second NIC and the test client's NIC.
2. Assign static IP `192.168.99.1/24` to the server NIC.
3. Start `nkb-server.exe` with ProxyDHCP enabled.
4. Power on the client workstation with UEFI network boot enabled.

---

## 8. BINARY DEPLOYMENT & COMMAND QUICK REFERENCE

All three compiled binaries are located in the `bin/` directory:

| Binary | Role | Usage Command |
| :--- | :--- | :--- |
| `bin\nkb-server.exe` | Core API, PXE, TFTP, SPA | `.\bin\nkb-server.exe -port 8080 -storage storage` |
| `bin\nkb-agent.exe` | Workstation Service | `.\bin\nkb-agent.exe -debug` (Interactive) or `.\bin\nkb-agent.exe -service install` |
| `bin\nkb-desktop-manager.exe` | Wails Desktop GUI | `.\bin\nkb-desktop-manager.exe` |

