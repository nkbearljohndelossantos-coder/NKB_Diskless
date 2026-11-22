package pxe_boot

import (
	"fmt"
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
}

// GenerateScript produces the dynamic iPXE configuration script based on client state in NKB database.
func GenerateScript(req BootRequest, db *database.DB) (string, error) {
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

	if client.AssignedDiskID != nil && *client.AssignedDiskID != "" {
		diskID = *client.AssignedDiskID
		d, _ := db.GetDiskByID(diskID)
		if d != nil {
			diskName = d.Name
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
echo Connecting to NKB Virtual Block SAN...
sanhook iscsi:%s::::%s || goto san_fail
echo Booting Windows Operating System from SAN...
sanboot --no-describe iscsi:%s::::%s || goto san_fail

:san_fail
echo [FATAL] SAN connection failed to target: %s
echo Checking write-cache allocation and iSCSI target daemon...
prompt --timeout 15000 Press any key to retry or wait 15 seconds to reboot...
reboot
`, client.Hostname, client.MACAddress, diskName, client.WriteCacheMode,
		req.ServerIP, targetIQN,
		req.ServerIP, targetIQN,
		targetIQN)
}

func generateSuperClientScript(req BootRequest, client *models.Client, db *database.DB) string {
	var diskName string = "Golden Master Image"
	if client.AssignedDiskID != nil {
		d, _ := db.GetDiskByID(*client.AssignedDiskID)
		if d != nil {
			diskName = d.Name
		}
	}

	// Super-client target IQN points directly to the writable snapshot
	targetIQN := fmt.Sprintf("%s:superclient-%s", req.ISCSIPrefix, strings.ToLower(strings.ReplaceAll(client.MACAddress, ":", "")))

	return fmt.Sprintf(`#!ipxe
echo ========================================================
echo  *** NKB SUPER-CLIENT MAINTENANCE MODE ACTIVATED ***
echo  Station: %s (%s)
echo  Modifying Image: %s
echo ========================================================
echo [WARNING] All disk modifications will be persisted to the snapshot!
echo Ensure you install drivers/software and run sysprep before sealing!
set keep-san 1
sanhook iscsi:%s::::%s || goto super_fail
sanboot --no-describe iscsi:%s::::%s || goto super_fail

:super_fail
echo [FATAL] Super-Client failed to attach writable snapshot target: %s
prompt --timeout 15000 Press any key to reboot...
reboot
`, client.Hostname, client.MACAddress, diskName,
		req.ServerIP, targetIQN,
		req.ServerIP, targetIQN,
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
