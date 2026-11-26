# NKB DISKLESS SYSTEM — INSTALLATION GUIDE
Version 1.0.0 (Windows 11 Pro x64)

---

### 1. SYSTEM REQUIREMENTS

- **Operating System:** Windows 11 Pro 64-bit (Build 22H2 or later)
- **Processor:** x64 quad-core CPU (2.0 GHz or higher)
- **RAM:** 8 GB minimum (16 GB+ recommended for large client pools)
- **Network Hardware:**
  - **NIC 1 (Management):** Connected to corporate LAN / router with internet connectivity.
  - **NIC 2 (Dedicated Diskless):** Connected exclusively to an isolated unmanaged Gigabit switch.
  - **Isolated Switch:** Must NOT have uplinks to the corporate network or DHCP servers.
- **Storage:**
  - 10 GB free space for OS and application installation (`C:\Program Files\NKB Diskless System`).
  - Dedicated storage partition or NVMe/SSD drive for diskless images and client writebacks (`C:\NKB-PoC` or custom).

---

### 2. INSTALLATION STEPS

#### Step 1: Run the Installer
1. Right-click `NKB-Diskless-System-Setup-x64.exe` and select **Run as administrator**.
2. Review the welcome screen and choose the destination folder (default: `C:\Program Files\NKB Diskless System`).

#### Step 2: Select Components
- **Core Services & Binaries:** Required (installs `nkb-server`, `nkb-agent`, `nkb-discover`, `nkb-desktop-manager`).
- **Web Management Dashboard:** Installs the responsive web interface.
- **PXE & iPXE Network Bootloaders:** Installs `undionly.kpxe` (BIOS) and `ipxe.efi` (UEFI).
- **Hardware Discovery & Staging Scripts:** Installs PowerShell staging and rollback tools.

#### Step 3: Configure Optional Tasks
- **Create Desktop Shortcuts:** Adds a shortcut for NKB Desktop Manager.
- **Configure Windows Defender Firewall rules:** (Recommended) Automatically adds inbound allow rules for DHCP (UDP 67), ProxyDHCP (UDP 4011), TFTP (UDP 69), HTTP Boot (TCP 8080), API (TCP 8443), and iSCSI (TCP 3260) under group `NKB Diskless System`.
- **Register Windows Service:** (Optional) Registers `nkb-server.exe` as service `NKBDisklessServer` with **Manual** startup type.

#### Step 4: Complete Installation
Click **Install**. Once files are copied, ensure the option **Launch First-Run Hardware Setup Wizard** is checked, then click **Finish**.

---

### 3. FIRST-RUN HARDWARE ONBOARDING

The First-Run Setup Wizard automatically executes:
1. **Network Discovery:** Scans all physical host adapters and identifies their MAC address, link speed, and IP configuration.
2. **Management Interface Protection:** Automatically detects the adapter with an active default gateway (e.g. Wi-Fi or primary LAN) and flags it as **PROTECTED**.
3. **Dedicated NIC Selection:** Prompts you to select the dedicated secondary NIC intended for the isolated diskless switch.
4. **Configuration Generation:** Creates `config\network_profile.json` and `config\nkb_config.json` bound to the selected adapter's actual hardware identity.
5. **Safety Confirmation:** Validates that no network settings have been changed and that all PXE listeners remain **STOPPED**.

---

### 4. POST-INSTALLATION VERIFICATION

Open an administrative PowerShell prompt to verify that the installation is clean and safe:

```powershell
# 1. Verify all PXE listeners are stopped
Get-NetUDPEndpoint | Where-Object { $_.LocalPort -in @(67, 4011, 69) }
Get-NetTCPConnection | Where-Object { $_.LocalPort -in @(8080, 8443) }

# 2. Verify Windows host IP addresses are unchanged
Get-NetIPAddress -AddressFamily IPv4 | Select-Object InterfaceAlias, IPAddress

# 3. Start Management Web Dashboard (Optional manual test)
cd "C:\Program Files\NKB Diskless System"
.\bin\nkb-server.exe -config .\config\nkb_config.json
```
Navigate to `http://localhost:8443` in your browser to verify API and dashboard health.

---

### 5. ACTIVATION FOR PHYSICAL PXE BOOT

When physical hardware staging has been reviewed by the operator:
1. Ensure the dedicated NIC is physically cabled to the isolated unmanaged switch.
2. Ensure the test workstation has SATA/NVMe storage physically disconnected.
3. Review and execute the staging script:
   ```powershell
   powershell.exe -ExecutionPolicy Bypass -File "C:\Program Files\NKB Diskless System\config\scripts\stage_adapter.ps1"
   ```
4. If recovery is needed at any time, execute:
   ```powershell
   powershell.exe -ExecutionPolicy Bypass -File "C:\Program Files\NKB Diskless System\config\scripts\rollback_adapter.ps1"
   ```
