package main

import (
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"nkb-diskless/internal/services/pxe_boot"
)

func getLocalIPv4() (string, error) {
	// Find active physical non-loopback IPv4
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				if ip4 := ipnet.IP.To4(); ip4 != nil {
					// Prefer 192.168.x.x
					if ip4[0] == 192 && ip4[1] == 168 {
						return ip4.String(), nil
					}
				}
			}
		}
	}
	return "192.168.254.100", nil
}

func main() {
	defaultIP, _ := getLocalIPv4()

	bindIP := flag.String("ip", defaultIP, "Server IP address for PXE/TFTP/DHCP")
	bootDir := flag.String("boot-dir", `C:\NKB-PoC\boot`, "Root directory containing bootloader files")
	httpPort := flag.Int("http-port", 8443, "HTTP port for iPXE scripts")
	mode := flag.String("mode", "all", "Mode: 'standalone' (port 67), 'proxy' (port 4011), or 'all' (both)")
	flag.Parse()

	fmt.Println("=================================================================")
	fmt.Println("   NKB DISKLESS SYSTEM — EMBEDDED PXE & TFTP BOOT DAEMON        ")
	fmt.Println("=================================================================")
	fmt.Printf(" Server IP       : %s\n", *bindIP)
	fmt.Printf(" Boot Directory  : %s\n", *bootDir)
	fmt.Printf(" HTTP Script Port: %d\n", *httpPort)
	fmt.Printf(" Mode            : %s\n", *mode)
	fmt.Println("=================================================================")

	// 1. Start TFTP Server on UDP port 69
	tftpServer, err := pxe_boot.NewTFTPServer(*bindIP, *bootDir, 69)
	if err != nil {
		slog.Error("Failed to initialize TFTP server", "err", err)
		os.Exit(1)
	}

	if err := tftpServer.Start(); err != nil {
		slog.Error("Failed to start TFTP server on UDP port 69 (Requires Administrator)", "err", err)
		os.Exit(1)
	}
	defer tftpServer.Stop()
	fmt.Println("[✓] TFTP Boot Service active on UDP port 69")

	// 2. Start DHCP Server (Standalone port 67 and/or Proxy port 4011)
	var standaloneDHCP *pxe_boot.StandaloneDHCPServer
	var proxyDHCP *pxe_boot.ProxyDHCPServer

	if *mode == "standalone" || *mode == "all" {
		ip := net.ParseIP(*bindIP).To4()
		startLease := fmt.Sprintf("%d.%d.%d.150", ip[0], ip[1], ip[2])
		endLease := fmt.Sprintf("%d.%d.%d.200", ip[0], ip[1], ip[2])

		sDHCP, err := pxe_boot.NewStandaloneDHCPServer(*bindIP, 67, *httpPort, startLease, endLease)
		if err == nil {
			if err := sDHCP.Start(); err == nil {
				standaloneDHCP = sDHCP
				defer standaloneDHCP.Stop()
				fmt.Printf("[✓] Standalone DHCP Service active on UDP port 67 (Leases: %s - %s)\n", startLease, endLease)
			} else {
				slog.Warn("Port 67 in use or unavailable; fallback to ProxyDHCP mode", "err", err)
			}
		}
	}

	if *mode == "proxy" || *mode == "all" || standaloneDHCP == nil {
		pDHCP, err := pxe_boot.NewProxyDHCPServer(*bindIP, 4011, *httpPort)
		if err == nil {
			if err := pDHCP.Start(); err == nil {
				proxyDHCP = pDHCP
				defer proxyDHCP.Stop()
				fmt.Println("[✓] ProxyDHCP Service active on UDP port 4011")
			} else {
				slog.Warn("Failed to start ProxyDHCP on port 4011", "err", err)
			}
		}
	}

	fmt.Println("\n>>> READY! Power on or restart your Trial PC to boot over LAN.")
	fmt.Println(">>> Press Ctrl+C to stop the PXE daemon.")

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	fmt.Println("\nStopping PXE Boot Services...")
}
