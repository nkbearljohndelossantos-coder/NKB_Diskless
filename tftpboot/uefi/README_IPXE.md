# UEFI iPXE Bootloader Status & Verification Guide

## 1. Current File Status: CORRUPTED (NON-EXECUTABLE)
- **Path:** `tftpboot/uefi/ipxe.efi`
- **File Size:** 9,379 bytes
- **SHA-256 Hash:** `B620507312C5E97566A3C6CFAF99144FEFC18A0DA7D941401DFA0F5F58FB0368`
- **Current File Type:** ASCII/UTF-8 HTML Document (`<!DOCTYPE html>`, `<title>Page not found &middot; GitHub Pages</title>`)
- **Status:** **DEFECT CONFIRMED**. This file was saved during initial setup from a failed HTTP request (GitHub Pages 404 error page). It cannot be loaded by UEFI firmware.

## 2. Policy Enforcement
Under the strict Phase 3A.1 safety directive:
- **DO NOT EXECUTE** this file. Attempting to execute this file in UEFI firmware will result in an immediate boot crash or `Invalid Image` error.
- **DO NOT REPLACE** this file with an unverified third-party or untrusted binary. Replacement must strictly follow the acquisition and verification protocol documented below prior to physical boot testing in Phase 3B.

## 3. Official Acquisition Protocol for Phase 3B Preparation

To obtain a legitimate, verified UEFI x64 iPXE bootloader, use one of the two official methods:

### Method A: Official Pre-Compiled UEFI Binary
1. **Source:** Download directly from the official iPXE boot infrastructure:
   ```
   https://boot.ipxe.org/ipxe.efi
   ```
2. **Architecture:** Ensure the downloaded binary is built for x86-64 UEFI (`bin-x86_64-efi/ipxe.efi`).

### Method B: Self-Compiled iPXE Binary (Recommended for Custom Embedded Script)
1. **Source Repository:** Clone the official iPXE git repository:
   ```bash
   git clone https://github.com/ipxe/ipxe.git
   cd ipxe/src
   ```
2. **Build Target:** Compile specifically for UEFI x64:
   ```bash
   make bin-x86_64-efi/ipxe.efi
   ```

## 4. Verification Requirements Prior to Placement

Before replacing `tftpboot/uefi/ipxe.efi`:

1. **PE32+ Header Verification:**
   The file must be a genuine PE32+ executable for EFI byte code or AMD64:
   - First 2 bytes (Magic): `0x4D 0x5A` (`MZ`)
   - PE Header Offset at `0x3C`: Points to `PE\0\0`
   - Machine Type: `0x8664` (AMD64 / x86-64)
   - Subsystem: `0x000A` (EFI Application)

2. **File Size Check:**
   Legitimate iPXE UEFI binaries typically range between 900 KB and 1.2 MB. Files under 100 KB are indicative of incomplete downloads or HTML error documents.

3. **Checksum Recording:**
   Calculate and record the SHA-256 checksum in the project audit log:
   ```powershell
   Get-FileHash -Algorithm SHA256 .\tftpboot\uefi\ipxe.efi
   ```

4. **Safety Isolation Gate:**
   No bootloader shall be loaded into production networks. PXE testing must occur strictly on isolated hardware with dedicated physical NICs disconnected from corporate infrastructure.
