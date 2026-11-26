# NKB DISKLESS SYSTEM — UNINSTALLATION & RECOVERY GUIDE
Version 1.0.0 (Windows 11 Pro x64)

---

### 1. OVERVIEW

The NKB Diskless System provides a clean, non-destructive uninstallation process. By default, uninstallation removes application binaries, services, shortcuts, and firewall rules while **preserving all user data, diskless OS images, client writeback caches, and database records**.

---

### 2. UNINSTALLATION PROCEDURE

#### Method A: Windows Settings (Recommended)
1. Open Windows **Settings** (`Win + I`).
2. Navigate to **Apps** -> **Installed apps**.
3. Search for **NKB Diskless System**.
4. Click the three dots (`...`) and select **Uninstall**.
5. When prompted by User Account Control (UAC), click **Yes**.
6. The uninstaller will:
   - Stop and unregister the `NKBDisklessServer` Windows Service (if installed).
   - Remove all Windows Defender Firewall rules created under group `NKB Diskless System`.
   - Remove all binaries and static web files from `C:\Program Files\NKB Diskless System`.
   - Remove Start Menu and Desktop shortcuts.

#### Method B: Direct Uninstaller Execution
1. Open File Explorer and navigate to `C:\Program Files\NKB Diskless System`.
2. Right-click `unins000.exe` and select **Run as administrator**.
3. Follow the uninstallation prompts.

#### Method C: Silent Headless Uninstallation
For automated enterprise administration:
```cmd
"C:\Program Files\NKB Diskless System\unins000.exe" /VERYSILENT /SUPPRESSMSGBOXES /LOG="C:\Temp\nkb_uninstall.log"
```

---

### 3. USER DATA PRESERVATION POLICY

The uninstaller intentionally preserves:
- **Storage Root Directory (`C:\NKB-PoC` or custom path):** Contains golden images, client virtual disks, and writeback caches.
- **SQLite Database (`<StorageRoot>\database\nkb_diskless.db`):** Contains client registrations, workstation groups, and audit history.
- **Persistent Network Snapshots (`<InstallDir>\config\snapshots\`):** Contains historical pre-staging network snapshots for forensic or recovery purposes.

---

### 4. COMPLETE PURGE (OPTIONAL CLEAN WIPE)

If you wish to completely wipe all traces of the system after running the uninstaller:

1. Roll back network staging (if staged):
   ```powershell
   # If rollback script was backed up:
   powershell.exe -ExecutionPolicy Bypass -File "C:\NKB-PoC\config\scripts\rollback_adapter.ps1"
   ```
2. Delete the storage root folder:
   ```powershell
   Remove-Item -Path "C:\NKB-PoC" -Recurse -Force
   ```
3. Remove any remaining configuration directories:
   ```powershell
   Remove-Item -Path "C:\Program Files\NKB Diskless System" -Recurse -Force -ErrorAction SilentlyContinue
   ```
