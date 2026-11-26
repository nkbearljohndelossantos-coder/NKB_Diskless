package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"nkb-diskless/internal/services/network_mgmt"
)

func main() {
	fmt.Println("=== Initializing Phase 3E.1 Baseline Profile, Snapshots & Scripts ===")

	roots := []string{
		"C:\\Users\\earlj\\Desktop\\NKB Diskless System Management",
		"C:\\NKB-PoC",
	}

	for _, root := range roots {
		mgr, err := network_mgmt.NewNetworkConfigManager(root)
		if err != nil {
			log.Fatalf("Manager error for %s: %v", root, err)
		}

		profile, err := mgr.LoadProfile()
		if err != nil {
			log.Fatalf("Load profile error for %s: %v", root, err)
		}

		// Ensure baseline selected adapter properties are populated with real GUID
		persistentKey := network_mgmt.ComputePersistentKey("B0-0C-D1-E6-53-31", "{620271F0-ABF2-492B-B226-1871CF04E0BB}", "Realtek Gaming GbE Family Controller")
		profile.SelectedAdapter = network_mgmt.AdapterMatch{
			PersistentKey: persistentKey,
			Alias:         "Ethernet",
			Description:   "Realtek Gaming GbE Family Controller",
			Index:         8,
			GUID:          "{620271F0-ABF2-492B-B226-1871CF04E0BB}",
			MAC:           "B0-0C-D1-E6-53-31",
		}
		profile.Status = network_mgmt.StatusSelected
		profile.LastUpdatedAt = time.Now().UTC()

		if err := mgr.SaveProfile(profile); err != nil {
			log.Fatalf("Save error for %s: %v", root, err)
		}

		fmt.Printf("[%s] Profile saved to %s\n", root, mgr.ConfigFile())

		// Generate baseline synthetic template snapshot and rollback script
		sampleAdapter := network_mgmt.NetworkAdapter{
			ID:                   persistentKey,
			InterfaceAlias:       "Ethernet",
			InterfaceIndex:       8,
			InterfaceGUID:        "{620271F0-ABF2-492B-B226-1871CF04E0BB}",
			MACAddress:           "B0:0C:D1:E6:53:31",
			InterfaceDescription: "Realtek Gaming GbE Family Controller",
			DHCPEnabled:          true,
			IPv4Addresses:        []string{"192.168.254.20/24"},
			DefaultGateways:      []string{"192.168.254.254"},
			DNSServers:           []string{"192.168.254.254", "1.1.1.1"},
			Status:               "Up",
			NetworkCategory:      "Public",
		}

		snap := mgr.BuildSnapshotFromAdapter(&sampleAdapter)
		snap.SnapshotID = "snap_sample_synthetic_template"
		snap.IsSynthetic = true
		snap.Provenance = "SYNTHETIC_TEST_FIXTURE"
		snap.HostIdentity = "SYNTHETIC_TEMPLATE_HOST"
		snap.IntegrityHash = network_mgmt.ComputeSnapshotHash(snap)

		if err := mgr.SaveWindowsSnapshot(snap); err != nil {
			log.Printf("[%s] Error saving snapshot: %v", root, err)
		} else {
			fmt.Printf("[%s] Snapshot saved: %s/config/snapshots/%s.json\n", root, root, snap.SnapshotID)
		}

		// Generate elevated transactional staging script
		script := mgr.GenerateElevatedStagingScript(profile)
		scriptPath := fmt.Sprintf("%s\\config\\scripts\\stage_adapter.ps1", root)
		if err := os.WriteFile(scriptPath, []byte(script), 0644); err != nil {
			log.Printf("Error writing script to %s: %v", scriptPath, err)
		} else {
			fmt.Printf("[%s] Generated transactional staging script: %s\n", root, scriptPath)
		}

		// Generate elevated rollback script
		rollbackScript := mgr.GenerateRollbackScript(snap)
		rollbackPath := fmt.Sprintf("%s\\config\\scripts\\rollback_adapter.ps1", root)
		if err := os.WriteFile(rollbackPath, []byte(rollbackScript), 0644); err != nil {
			log.Printf("Error writing rollback script to %s: %v", rollbackPath, err)
		} else {
			fmt.Printf("[%s] Generated rollback script: %s\n", root, rollbackPath)
		}
	}
	fmt.Println("=== Initialization Complete ===")
}
