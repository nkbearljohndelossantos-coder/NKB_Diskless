package main

import (
	"fmt"
	"os"
	"strings"

	"nkb-diskless/internal/services/pxe_boot"
)

func main() {
	fmt.Println("================================================================================")
	fmt.Println(" NKB DISKLESS SYSTEM — NETWORK INTERFACE DISCOVERY & CLASSIFICATION (CLI)")
	fmt.Println(" Mode: READ-ONLY AUDIT")
	fmt.Println("================================================================================")

	ifaces, err := pxe_boot.DiscoverNetworkInterfaces()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error discovering interfaces: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("%-32s %-12s %-18s %-16s %-10s %s\n", "NAME", "STATUS", "IP ADDRESS", "GATEWAY", "TYPE", "SAFETY CLASSIFICATION")
	fmt.Println(strings.Repeat("-", 110))

	var dedicatedNIC *pxe_boot.NetworkInterfaceInfo
	var protectedNICs []pxe_boot.NetworkInterfaceInfo

	for _, iface := range ifaces {
		ip := "None"
		if len(iface.IPv4Addresses) > 0 {
			ip = strings.Join(iface.IPv4Addresses, ", ")
		}
		gw := "None"
		if iface.Gateway != "" {
			gw = iface.Gateway
		}

		adapterType := "Virtual"
		if iface.IsPhysical {
			adapterType = "Physical"
		}

		classification := "Normal"
		if iface.IsDedicatedTestNIC {
			classification = "[TARGET TEST NIC]"
			dedicatedNIC = &iface
		} else if iface.IsProtected {
			classification = "[PROTECTED PRODUCTION]"
			protectedNICs = append(protectedNICs, iface)
		} else if iface.OperStatus == "Disconnected" {
			classification = "[DISCONNECTED]"
		}

		fmt.Printf("%-32s %-12s %-18s %-16s %-10s %s\n",
			truncate(iface.Name, 31),
			iface.OperStatus,
			truncate(ip, 17),
			truncate(gw, 15),
			adapterType,
			classification,
		)
	}

	fmt.Println("================================================================================")
	if dedicatedNIC != nil {
		fmt.Printf("[✓] Dedicated PXE Server NIC Detected: %s (%v)\n", dedicatedNIC.Name, dedicatedNIC.IPv4Addresses)
	} else {
		fmt.Println("[!] NO DEDICATED TEST NIC (192.168.50.X) DETECTED!")
		fmt.Println("    To prepare for Phase 3B physical staging:")
		fmt.Println("    1. Attach secondary dedicated Gigabit Ethernet adapter.")
		fmt.Println("    2. Configure static IPv4: 192.168.50.1 / 255.255.255.0 (No Default Gateway).")
		fmt.Println("    3. Connect patch cable directly to isolated air-gapped test switch.")
	}

	if len(protectedNICs) > 0 {
		fmt.Println("\n[i] Protected Production Adapters (Network boot binding strictly blocked):")
		for _, p := range protectedNICs {
			fmt.Printf("    - %s (%s, GW: %s)\n", p.Name, strings.Join(p.IPv4Addresses, ", "), p.Gateway)
		}
	}
	fmt.Println("================================================================================")
}

func truncate(s string, max int) string {
	if len(s) > max {
		return s[:max-3] + "..."
	}
	return s
}
