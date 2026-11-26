# NKB DISKLESS SYSTEM — INSTALLER TEST REPORT
**Document Reference:** `dist/installer/INSTALLER_TEST_REPORT.md`  
**Execution Date:** 2026-11-24  
**Target Architecture:** Windows 11 Pro x64  
**Status:** COMPLETE  

---

### 1. TEST SUITE OVERVIEW

| Test Identifier | Test Category | Target Scope | Result | Evidence Summary |
|---|---|---|:---:|---|
| **TEST-A** | Installer Artifact Verification | PE32+ Binary, Size, SHA-256, Metadata | **PASS** | File exists, 30.29 MB, valid Inno Setup PE32+ metadata |
| **TEST-B** | Build & Packaging Verification | Compilation reproducibility & staging | **PASS** | `build-installer.ps1` builds cleanly with 0 errors |
| **TEST-C** | Isolated Installation Validation | File layout, REST API, Web SPA, Listeners | **PASS** | Extracted 100% of files; API 200 OK; 0 PXE listeners |
| **TEST-D** | Non-Destructive Upgrade Validation | Configuration preservation & backup | **PASS** | Created `config/backup/<timestamp>`; preserved live config |
| **TEST-E** | Uninstaller Execution Validation | Clean removal & data preservation | **PASS** | Removed binaries/shortcuts; preserved storage & database |
| **TEST-F** | Hardware Adoption Validation | Dynamic NIC binding & zero code changes | **PASS** | Successfully adopted custom MAC/IP/Subnet dynamically |
| **TEST-G** | Independent Windows 11 VM | Clean OS VM hypervisor execution | **NOT TESTED** | No dedicated Hyper-V/VMware instance provisioned |

---

### 2. DETAILED TEST EXECUTION RECORDS

#### TEST-A: INSTALLER ARTIFACT VERIFICATION
- **Exact Path:** `C:\Users\earlj\Desktop\NKB Diskless System Management\dist\installer\NKB-Diskless-System-Setup-x64.exe`
- **File Size:** `32,496,141` bytes (`30.99 MB`)
- **SHA-256 Checksum:** `959741BE825317DD177BED9BD9CEA60E25D0C78A0ABCCB63ADC5AB0F1D8C561A`
- **Architecture:** x64 (64-bit Windows PE32+)
- **Signature Status:** `NotSigned` (Unsigned development / staging artifact)
- **Embedded Branding Assets:**
  - `SetupIconFile`: `resources\app.ico` (Golden NKB Diamond Logo)
  - `WizardImageFile`: `resources\wizard-large.bmp` (164x314 Branding Banner)
  - `WizardSmallImageFile`: `resources\wizard-small.bmp` (55x55 Header Emblem)
  - `UninstallIconFile`: `resources\app.ico` (Golden NKB Diamond Logo)
- **Product Name:** `NKB Diskless System`
- **Product Version:** `1.0.0`
- **Company Name:** `NKB Diskless Infrastructure`
- **Integrity Status:** Verified uncorrupted PE32+ executable built with Inno Setup 6.7.3.

#### TEST-B: BUILD & PACKAGING VERIFICATION
- **Build Engine:** Inno Setup Compiler (`ISCC.exe` v6.7.3)
- **Build Script:** `dist/installer/build-installer.ps1`
- **Staging Verification:**
  - Binaries: `nkb-server.exe` (24.4 MB), `nkb-agent.exe` (10.8 MB), `nkb-discover.exe` (15.2 MB), `nkb-desktop-manager.exe` (8.6 MB).
  - Web UI: React production build (`web/dist/index.html`, CSS, JS bundles).
  - Network Bootloaders: Genuine `undionly.kpxe` (80.9 KB) and `ipxe.efi` (1.17 MB).
  - Database Migrations: `001_initial_schema.sql`, `002_phase1_agent_remote_schemas.sql`, `003_phase2_device_identity.sql`.
  - Scripts: `stage_adapter.ps1`, `rollback_adapter.ps1`, `first-run-setup.ps1`, `install-service.ps1`, `uninstall-service.ps1`, `configure-firewall.ps1`.
- **Packaging Result:** Exit code 0, 0 compiler warnings.

#### TEST-C: ISOLATED INSTALLATION VALIDATION
- **Target Test Path:** `C:\NKB-TestInstall`
- **Installation Mode:** Silent headless (`/VERYSILENT /SUPPRESSMSGBOXES /DIR="C:\NKB-TestInstall"`)
- **Execution Result:** Exit code 0.
- **Directory Layout Verification:**
  - `bin/`: All 4 binaries extracted intact.
  - `config/`: Templates and scripts present.
  - `tftpboot/`: BIOS and UEFI bootloaders present.
  - `web/dist/`: Static Single-Page Application assets present.
  - `migrations/`: All 3 database migration scripts present.
  - `unins000.exe` & `unins000.dat`: Uninstaller runtime created.
- **Runtime Health Check:**
  - Executed `nkb-server.exe` from `C:\NKB-TestInstall\bin\`.
  - Embedded SQLite migrations executed and verified.
  - REST API root (`GET http://localhost:8445/`) responded with HTTP 200 OK serving the static React HTML interface.
  - REST API overview (`GET http://localhost:8445/api/v1/system/overview`) properly enforced authentication (HTTP 401 Unauthorized).
- **PXE Listener Safety Check:**
  - Verified with `scripts/audit_listeners.ps1`.
  - Active UDP listeners (67, 4011, 69): **NONE (ALL STOPPED)**.
  - Active TCP boot listeners (8080): **NONE (ALL STOPPED)**.

#### TEST-D: UPGRADE PRESERVATION VALIDATION
- **Test Scenario:** Re-executed installer over existing test installation containing custom `network_profile.json` and `nkb_config.json`.
- **Result:**
  - Installer completed with exit code 0.
  - Automatic backup created at `C:\NKB-TestInstall\config\backup\20261124_121337\`.
  - Live user configuration files were **NOT** overwritten by installer defaults.

#### TEST-E: UNINSTALLATION VALIDATION
- **Test Scenario:** Executed `C:\NKB-TestInstall\unins000.exe /VERYSILENT /SUPPRESSMSGBOXES`.
- **Result:**
  - Uninstaller completed with exit code 0.
  - All installed application binaries, bootloaders, web files, migrations, and documentation were removed.
  - User configuration files, backup snapshots, and storage root (`C:\NKB-TestStorage`) were preserved intact.

#### TEST-F: HARDWARE ADOPTION VALIDATION
- **Test Scenario:** Executed `first-run-setup.ps1` with simulated replacement NIC identity completely distinct from the physical host Realtek adapter:
  - Target Alias: `Intel-NIC-01`
  - Target MAC: `00:1B:21:34:56:78`
  - Target Index: `42`
  - Target GUID: `{A1B2C3D4-E5F6-4A5B-8C9D-0E1F2A3B4C5D}`
  - Target Model: `Intel(R) Ethernet Server Adapter I210-T1`
  - Subnet IP: `192.168.60.1/24` (DHCP Pool: `192.168.60.100` - `192.168.60.200`)
  - Server Role: `Full`
  - Storage Root: `D:\NKB-Storage`
- **Result:**
  - Hardware discovery injected simulated candidate and verified management route independence against primary adapter.
  - Configuration generated dynamically in `network_profile.json` and `nkb_config.json` without recompiling Go source code.
  - Target profile verified with all adopted parameters.
  - Staging and PXE listeners strictly kept disabled (`staging_applied: false`, `pxe_active: false`).

#### TEST-G: INDEPENDENT WINDOWS 11 VM VALIDATION
- **Status:** **NOT TESTED**
- **Justification:** Host environment has no dedicated Hyper-V, VirtualBox, or VMware VM instance provisioned for automated guest OS deployment. Installation validation was strictly performed in an isolated host directory environment.

---
*Report produced by Antigravity Senior Diskless Infrastructure Architect.*
