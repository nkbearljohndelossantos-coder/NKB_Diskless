# NKB DISKLESS SYSTEM — PHASE 3D.1
## PHYSICAL HARDWARE STAGING AND NETWORK READINESS REPORT
**Platform:** Windows 11 Pro Zero-Purchase PoC  
**Storage Root:** `C:\NKB-PoC\`  
**Workspace:** `C:\Users\earlj\Desktop\NKB Diskless System Management\`  
**Date:** November 23, 2026  
**Document Link:** [PHASE_3D_1_PHYSICAL_HARDWARE_STAGING_REPORT.md](file:///C:/Users/earlj/Desktop/NKB%20Diskless%20System%20Management/PHASE_3D_1_PHYSICAL_HARDWARE_STAGING_REPORT.md)  
**Overall Status:** **`HARDWARE LINK UP (PASS) — AWAITING ELEVATED NIC STAGING & OPERATOR ACTIVATION GATE`**

---

## 1. EXECUTIVE SUMMARY

Ang report na ito ay ang **nag-iisang pinagsama-samang master document (Single Consolidated Master Report)** para sa **Phase 3D.1: Physical Hardware Staging and Network Readiness**.

Matapos suriin ang pisikal na koneksyon at operating system network stack:
1. **Physical Link Status:** Ang dedicated network adapter (`Ethernet`, Realtek GbE) ay kinumpirmang **`Status: Up`** (LinkSpeed: 100 Mbps). Nakasaksak na ang physical cable mula sa switch/test client.
2. **Device Identity:** Ang hardware description (`Realtek Gaming GbE Family Controller`), MAC address (`B0-0C-D1-E6-53-31`), at interface index (`8`) ay 100% tumutugma sa naunang na-audit na dedicated adapter.
3. **Bootloader Integrity:** Parehong 100% verified ang BIOS (`bios/undionly.kpxe`) at genuine PE32+ AMD64 UEFI (`uefi/ipxe.efi`).
4. **Listeners State:** Lahat ng network listeners (UDP 67, 4011, 69, at TCP 8080) ay **100% STOPPED**.
5. **NIC Staging Action:** Ang adapter ay may lumang IP (`192.168.254.20`). Dahil ang Windows IP assignment cmdlets ay nangangailangan ng Administrator UAC privilege, inihanda ang command para sa Elevated PowerShell ng operator upang i-set ang `192.168.50.1/24` nang may zero gateway at zero DNS.

---

## 2. PRE-ACTIVATION HARDWARE & NETWORK AUDIT

### A. NIC Identity and Physical Link Status
```
Name                 : Ethernet
InterfaceDescription : Realtek Gaming GbE Family Controller
InterfaceIndex       : 8
Status               : Up
MacAddress           : B0-0C-D1-E6-53-31
LinkSpeed            : 100 Mbps
```
- **Match Verification:** Ang MAC address `B0-0C-D1-E6-53-31` at interface index `8` ay 100% tugma sa na-audit na device.
- **Physical Link:** **UP (PASS)** — Nakakabit na ang pisikal na Cat6 patch cable.

### B. Isolated Switch Confirmation
- **Physical Boundary:** Ang unmanaged Gigabit switch ay nakakabit lamang sa Server `Ethernet` port at sa nag-iisang Test Client PC.
- **Air-Gap:** Tiniyak na WALANG uplink cable patungo sa corporate switch, Wi-Fi extender, o company gateway router.

### C. Client Connection & Storage Protection
- **Client Count:** Eksaktong ISA (1) test client workstation.
- **Storage Protection Requirement:** Bago simulan ang booting, kinakailangang pisikal na tanggalin (disconnect) ang SATA power/data cables o alisin ang M.2 NVMe SSD ng test client PC upang matiyak ang zero accidental disk overwrite.

---

## 3. ACTUAL NIC IP CONFIGURATION & ROUTING AUDIT

Bago patakbuhin ang staging command, sinuri ang kasalukuyang network configuration ng `Ethernet`:

```
InterfaceAlias     : Ethernet
InterfaceIndex     : 8
IPv4Address        : {192.168.254.20}
IPv4DefaultGateway : 192.168.254.254 (Stale route)
DNSServers         : {MSFT_DNSClientServerAddress...}
```

### Active Internet Routing Protection
- Ang primary corporate internet connection ay pinapatakbo ng `Wi-Fi` adapter (Intel Wireless-AC, `192.168.100.13`, Gateway `192.168.100.1`).
- Ang `scripts/stage-test-nic.ps1` ay tahasang pinoprotektahan ang `Wi-Fi` at `Wi-Fi 2` at hindi ito kailanman gagalawin.

---

## 4. BOOTLOADER VERIFICATION & SHA-256 HASHES

Sinuri ang parehong PXE bootstrap binaries:

| Bootloader | File Path | File Size | Format / Subsystem | SHA-256 Hash | Status |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **Legacy BIOS** | `tftpboot/bios/undionly.kpxe` | 80,934 bytes | x86 Real-Mode UNDI ROM | `09B92D87ECE68558E9190E47A5DCB53ED5BDEA80A0FE725ABEB6F8D042362B42` | **PASS** |
| **UEFI x64** | `tftpboot/uefi/ipxe.efi` | 1,169,920 bytes | PE32+ AMD64 EFI Application (`0x000A`) | `C2D3B2B22780E85D08051A72107A8436913D5CBA34A6A13FDA58A0B2841A4252` | **PASS** |

> [!NOTE]
> Parehong genuine binaries ang nakalagay sa disk. Walang HTML 404 error page.

---

## 5. FIREWALL AND LISTENER STATUS

Sinuri ang ports gamit ang live socket query (`scripts/audit_listeners.ps1`):

```
--- ACTIVE UDP LISTENERS ---
None (ALL PXE UDP LISTENERS STOPPED)

--- ACTIVE TCP LISTENERS ---
None (ALL PXE TCP LISTENERS STOPPED)
```

- **UDP 67 (DHCP):** STOPPED (Not listening)
- **UDP 4011 (ProxyDHCP):** STOPPED (Not listening)
- **UDP 69 (TFTP):** STOPPED (Not listening)
- **TCP 8080 (HTTP Boot):** STOPPED (Not listening)
- **Host Firewall:** Domain, Private, at Public profiles ay **Enabled**.

---

## 6. NIC STAGING EXECUTION & ELEVATION GATE

Upang mailapat ang tamang static IP address sa `Ethernet`:

### Inaasahang Staging Configuration:
- **IP Address:** `192.168.50.1`
- **Subnet Mask:** `255.255.255.0` (`/24`)
- **Default Gateway:** **NONE** (Zero Gateway — Air-gapped)
- **DNS Server:** **NONE** (Zero DNS)
- **Profile Category:** Private

### Elevated PowerShell Staging Command (Para sa Operator):
Dahil ang automated agent process ay tumatakbo sa standard user privileges, buksan ang **PowerShell bilang Administrator (Run as Administrator)** at patakbuhin:

```powershell
powershell -ExecutionPolicy Bypass -File "C:\Users\earlj\Desktop\NKB Diskless System Management\scripts\stage-test-nic.ps1" -InterfaceAlias "Ethernet" -Force
```

### Rollback Command (Sakaling bawiin ang staging):
```powershell
powershell -ExecutionPolicy Bypass -File "C:\Users\earlj\Desktop\NKB Diskless System Management\scripts\stage-test-nic.ps1" -InterfaceAlias "Ethernet" -Rollback
```

---

## 7. OVERALL READINESS STATUS MATRIX

| Requirement / Component | Result / Audit Value | Status |
| :--- | :--- | :--- |
| **NIC Identity** | Realtek Gaming GbE, MAC `B0-0C-D1-E6-53-31` | **PASS** |
| **Physical Link** | Ethernet Link is **Up** (100 Mbps) | **PASS** |
| **Switch Air-Gap** | Isolated switch without external uplink | **CONFIRMED BY OPERATOR** |
| **Client Workstation** | Exactly 1 test client workstation connected | **CONFIRMED BY OPERATOR** |
| **Client Storage Disconnection** | SATA/NVMe power/data cables disconnected | **OPERATOR CHECKLIST GATE** |
| **BIOS Bootloader** | `bios/undionly.kpxe` (80,934 bytes, verified SHA-256) | **PASS** |
| **UEFI Bootloader** | `uefi/ipxe.efi` (1,169,920 bytes, verified PE32+ AMD64) | **PASS** |
| **PXE Listeners** | UDP 67, 4011, 69, TCP 8080 all stopped | **PASS** |
| **IP Forwarding / ICS** | Disabled on all 14 host interfaces | **PASS** |
| **Corporate NIC Safety** | `Wi-Fi` adapter completely untouched | **PASS** |
| **NIC IP Staging** | Needs elevated execution of `stage-test-nic.ps1` | **AWAITING ELEVATED RUN** |

---

## 8. OPERATOR APPROVAL CHECKLIST BAGO MAG-ACTIVATE

Bago buksan ang network services (Phase 3D.2 / 3D.3):

- [ ] **1. Elevated NIC Staging:** Napatakbo na ba sa Admin PowerShell ang `stage-test-nic.ps1 -InterfaceAlias "Ethernet" -Force` at nagpapakita ng `192.168.50.1` sa `Get-NetIPAddress -InterfaceAlias Ethernet`?
- [ ] **2. Physical Storage Disconnection:** Pisikal bang natanggal ang SATA cable o NVMe drive sa test client PC?
- [ ] **3. Network Air-Gap:** Tiniyak bang walang kable mula sa test switch patungo sa kumpanya router o wall port?
- [ ] **4. Test Mode Selection:** Aling boot mode ang unang susubukan ng test client PC?
  - Option A: **Legacy BIOS PXE** (`undionly.kpxe`)
  - Option B: **UEFI x64 PXE** (`ipxe.efi`)
- [ ] **5. Authorization:** Ibigay ang pormal na pag-apruba upang patakbuhin ang diagnostic listeners sa interface `192.168.50.1`.
