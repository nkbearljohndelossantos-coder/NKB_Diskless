package pxe_boot

import (
	"fmt"
	"os"
	"strings"

	"nkb-diskless/internal/database"
	"nkb-diskless/internal/models"
)

// BootRequest contains parameters passed by iPXE via HTTP query string.
type BootRequest struct {
	MAC         string
	IP          string
	Platform    string // "efi" or "pcbios"
	Arch        string // "x86_64", etc.
	ServerIP    string
	HTTPPort    int
	ISCSIPort   int
	ISCSIPrefix string
	Phase3AMode bool // Safety enforcement for Phase 3A isolated test
}

// GeneratePhase3AScript produces a safe, read-only diagnostic iPXE script for Phase 3A verification.
// CRITICAL SAFETY POLICY: Under NO circumstances may this script execute sanboot, sanhook,
// attach iSCSI targets, chainload operating systems, or modify any client storage.
func GeneratePhase3AScript(req BootRequest) string {
	macNorm := strings.ToUpper(strings.ReplaceAll(req.MAC, "-", ":"))
	return fmt.Sprintf(`#!ipxe
echo ========================================================
echo  NKB DISKLESS PLATFORM - PHASE 3A PXE TEST
echo ========================================================
echo Client MAC: %s
echo Client IP:  %s
echo Platform:   %s
echo Arch:       %s
echo Server IP:  %s
echo HTTP Port:  %d
echo ========================================================
echo [PHASE 3A] iPXE chainloading and script retrieval successful.
echo [PHASE 3A] Safe diagnostic mode active - NO DISKS ATTACHED.
echo [PHASE 3A] Network boot verification passed.
echo ========================================================
echo
prompt --key 0x02 --timeout 15000 Press Ctrl-B for the iPXE command line or wait to reboot... || goto reboot_node

:shell_prompt
shell
goto reboot_node

:reboot_node
echo Rebooting system in 5 seconds...
sleep 5
reboot
`, macNorm, req.IP, req.Platform, req.Arch, req.ServerIP, req.HTTPPort)
}

// GenerateScript produces the dynamic iPXE configuration script based on client state in NKB database.
func GenerateScript(req BootRequest, db *database.DB) (string, error) {
	// If Phase 3A Safe Mode is requested or active, strictly serve non-destructive diagnostic script
	if req.Phase3AMode {
		return GeneratePhase3AScript(req), nil
	}

	macNorm := strings.ToUpper(strings.ReplaceAll(req.MAC, "-", ":"))
	client, err := db.GetClientByMAC(macNorm)
	if err != nil {
		return generateErrorScript(fmt.Sprintf("Database query failed: %v", err)), nil
	}

	// Case 1: Unregistered workstation
	if client == nil {
		// Auto-register discovered computer
		_ = db.RecordHeartbeat(macNorm, req.IP, `{"status":"pending_registration"}`)
		return generateUnregisteredScript(req, macNorm), nil
	}

	// Update client heartbeat and last boot time
	_ = db.RecordHeartbeat(macNorm, req.IP, "")

	// Case 2: Super-Client Mode (Maintenance / Image Update)
	if client.IsSuperClient {
		return generateSuperClientScript(req, client, db), nil
	}

	// Case 3: Normal Diskless Boot
	return generateNormalBootScript(req, client, db), nil
}

func generateNormalBootScript(req BootRequest, client *models.Client, db *database.DB) string {
	var diskName string = "Default OS Image"
	var diskID string
	var hasOS bool = false

	if client.AssignedDiskID != nil && *client.AssignedDiskID != "" {
		diskID = *client.AssignedDiskID
		d, _ := db.GetDiskByID(diskID)
		if d != nil {
			diskName = d.Name
			if fi, err := os.Stat(d.FilePath); err == nil {
				// Sparse / unpartitioned freshly created virtual disks are < 1GB (~4MB).
				// Populated Windows OS installation images exceed 1GB (typically 8GB-25GB).
				if fi.Size() > 1024*1024*1024 {
					hasOS = true
				}
			}
		}
	} else if client.GroupID != nil && *client.GroupID != "" {
		// Fall back to group's default disk
		groups, _ := db.ListGroups()
		for _, g := range groups {
			if g.ID == *client.GroupID && g.DefaultDiskID != nil {
				diskID = *g.DefaultDiskID
				d, _ := db.GetDiskByID(diskID)
				if d != nil {
					diskName = d.Name
					if fi, err := os.Stat(d.FilePath); err == nil {
						if fi.Size() > 1024*1024*1024 {
							hasOS = true
						}
					}
				}
				break
			}
		}
	}

	if diskID == "" {
		return fmt.Sprintf(`#!ipxe
echo ========================================================
echo  NKB DISKLESS COMPUTING PLATFORM
echo  Client: %s (%s)
echo ========================================================
echo [ERROR] No virtual disk is assigned to this client or its group.
echo Please assign a disk in the NKB Administrator Dashboard.
echo Server Management URL: https://%s:%d
echo
prompt --key 0x02 --timeout 20000 Press Ctrl-B for the iPXE command line... || goto reboot_node
:reboot_node
reboot
`, client.Hostname, client.MACAddress, req.ServerIP, req.HTTPPort)
	}

	targetMode := "direct"
	modePrompt := "Windows OS detected -> Booting directly to Windows Desktop..."
	if !hasOS {
		targetMode = "winpe"
		modePrompt = "Empty/New Virtual Disk -> Launching Windows Network Installer (WinPE)..."
	}

	// Target IQN per client (e.g. iqn.2026-11.com.nkb:client-001a2b3c4d5e)
	targetIQN := fmt.Sprintf("%s:client-%s", req.ISCSIPrefix, strings.ToLower(strings.ReplaceAll(client.MACAddress, ":", "")))

	return fmt.Sprintf(`#!ipxe
echo ========================================================
echo  NKB DISKLESS COMPUTING PLATFORM
echo  Station: %s (%s)
echo  Active Disk: %s
echo  Write-Cache Mode: %s
echo ========================================================
set keep-san 1
set net0/gateway 0.0.0.0

echo [SMART DETECTION] %s
echo

menu NKB Boot Selection
item --default %s 1. [Auto] %s
item direct         2. Direct Windows 11 Desktop (Diskless iSCSI)
item winpe          3. Windows 11 Network Installer (WinPE)
item shell          4. iPXE Command Shell
choose --timeout 3000 --default %s target_mode && goto ${target_mode}

:direct
echo Connecting to NKB Virtual Block SAN and Booting Windows...
sanboot iscsi:%s::::%s || goto san_fail

:winpe
echo Attaching Virtual Disk SAN target for Windows installation...
sanhook iscsi:%s::::%s || goto san_fail
echo Loading Windows 11 Network Installer via wimboot...
kernel http://%s:8080/wimboot
initrd http://%s:8080/winpe/bootmgr bootmgr
initrd http://%s:8080/winpe/boot/bcd BCD
initrd http://%s:8080/winpe/boot/boot.sdi boot.sdi
initrd http://%s:8080/winpe/sources/boot.wim boot.wim
boot

:shell
shell

:san_fail
echo [FATAL] SAN connection failed to target: %s
echo Checking write-cache allocation and iSCSI target daemon...
prompt --timeout 15000 Press any key to retry or wait 15 seconds to reboot...
reboot
`, client.Hostname, client.MACAddress, diskName, client.WriteCacheMode,
		modePrompt,
		targetMode, modePrompt,
		targetMode,
		req.ServerIP, targetIQN,
		req.ServerIP, targetIQN,
		req.ServerIP, req.ServerIP, req.ServerIP, req.ServerIP, req.ServerIP,
		targetIQN)
}

func generateSuperClientScript(req BootRequest, client *models.Client, db *database.DB) string {
	var diskName string = "Golden Master Image"
	var hasOS bool = false
	if client.AssignedDiskID != nil {
		d, _ := db.GetDiskByID(*client.AssignedDiskID)
		if d != nil {
			diskName = d.Name
			if fi, err := os.Stat(d.FilePath); err == nil {
				if fi.Size() > 1024*1024*1024 {
					hasOS = true
				}
			}
		}
	}

	// Super-client target IQN points directly to the writable snapshot
	targetIQN := fmt.Sprintf("%s:superclient-%s", req.ISCSIPrefix, strings.ToLower(strings.ReplaceAll(client.MACAddress, ":", "")))

	targetMode := "direct"
	modePrompt := "Windows OS detected -> Booting writable snapshot directly..."
	if !hasOS {
		targetMode = "winpe"
		modePrompt = "Empty Golden Image -> Launching WinPE to Install Windows directly..."
	}

	return fmt.Sprintf(`#!ipxe
echo ========================================================
echo  *** NKB SUPER-CLIENT MAINTENANCE MODE ACTIVATED ***
echo  Station: %s (%s)
echo  Modifying Image: %s
echo ========================================================
echo [WARNING] All disk modifications will be persisted to the snapshot!
echo Ensure you install drivers/software and run sysprep before sealing!
set keep-san 1
set net0/gateway 0.0.0.0

echo [SMART DETECTION] %s
echo

menu NKB Super-Client Boot Selection
item --default %s 1. [Auto] %s
item direct         2. Direct Boot Windows (Writable SAN Target)
item winpe          3. Windows 11 Network Installer (WinPE Setup)
item shell          4. iPXE Command Shell
choose --timeout 3000 --default %s target_mode && goto ${target_mode}

:direct
echo Attaching writable snapshot SAN target and booting...
sanhook iscsi:%s::::%s || goto super_fail
sanboot --no-describe iscsi:%s::::%s || goto super_fail

:winpe
echo Attaching writable snapshot SAN target for installation...
sanhook iscsi:%s::::%s || goto super_fail
echo Loading Windows 11 Network Installer via wimboot...
kernel http://%s:8080/wimboot
initrd http://%s:8080/winpe/bootmgr bootmgr
initrd http://%s:8080/winpe/boot/bcd BCD
initrd http://%s:8080/winpe/boot/boot.sdi boot.sdi
initrd http://%s:8080/winpe/sources/boot.wim boot.wim
boot

:shell
shell

:super_fail
echo [FATAL] Super-Client failed to attach writable snapshot target: %s
prompt --timeout 15000 Press any key to reboot...
reboot
`, client.Hostname, client.MACAddress, diskName,
		modePrompt,
		targetMode, modePrompt,
		targetMode,
		req.ServerIP, targetIQN,
		req.ServerIP, targetIQN,
		req.ServerIP, targetIQN,
		req.ServerIP, req.ServerIP, req.ServerIP, req.ServerIP, req.ServerIP,
		targetIQN)
}

func generateUnregisteredScript(req BootRequest, mac string) string {
	return fmt.Sprintf(`#!ipxe
echo ========================================================
echo  NKB DISKLESS SYSTEM - UNREGISTERED WORKSTATION
echo  MAC Address: %s
echo ========================================================
echo This computer has been registered as pending in the NKB Dashboard.
echo An administrator must assign a group and virtual disk to enable booting.
echo
menu NKB Workstation Boot Options
item --default winpe  Boot NKB Hardware Diagnostics & Discovery (WinPE)
item shell            Open iPXE Command Console
item reboot           Reboot Station
choose --timeout 15000 --default winpe target_action && goto ${target_action}

:winpe
echo Loading NKB Hardware Registration Agent...
kernel http://%s:%d/boot/wimboot
initrd http://%s:%d/boot/winpe/bootmgr bootmgr
initrd http://%s:%d/boot/winpe/boot/bcd BCD
initrd http://%s:%d/boot/winpe/boot/boot.sdi boot.sdi
initrd http://%s:%d/boot/winpe/sources/boot.wim boot.wim
boot || goto reboot

:shell
shell

:reboot
reboot
`, mac, req.ServerIP, req.HTTPPort, req.ServerIP, req.HTTPPort, req.ServerIP, req.HTTPPort, req.ServerIP, req.HTTPPort, req.ServerIP, req.HTTPPort)
}

func generateErrorScript(errMessage string) string {
	return fmt.Sprintf(`#!ipxe
echo ========================================================
echo  NKB DISKLESS PLATFORM - SYSTEM ERROR
echo ========================================================
echo %s
echo
prompt --timeout 15000 Rebooting station in 15 seconds...
reboot
`, errMessage)
}
