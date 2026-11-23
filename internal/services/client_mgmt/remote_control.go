package client_mgmt

import (
	"encoding/hex"
	"fmt"
	"net"
	"strings"
)

// SendWakeOnLAN constructs and broadcasts an RFC magic packet to power on a sleeping PC.
func SendWakeOnLAN(macAddress string, broadcastIP string) error {
	cleanMAC := strings.ReplaceAll(macAddress, ":", "")
	cleanMAC = strings.ReplaceAll(cleanMAC, "-", "")

	if len(cleanMAC) != 12 {
		return fmt.Errorf("invalid MAC address: %s", macAddress)
	}

	macBytes, err := hex.DecodeString(cleanMAC)
	if err != nil {
		return fmt.Errorf("failed to parse MAC hex: %w", err)
	}

	// 6 bytes of 0xFF followed by 16 repetitions of the target MAC
	packet := make([]byte, 6+16*6)
	for i := 0; i < 6; i++ {
		packet[i] = 0xFF
	}
	for i := 0; i < 16; i++ {
		copy(packet[6+i*6:], macBytes)
	}

	if broadcastIP == "" {
		broadcastIP = "255.255.255.255"
	}
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:9", broadcastIP))
	if err != nil {
		return err
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = conn.Write(packet)
	return err
}

// GenerateRDPFile creates a standard Windows .rdp file content for native mstsc.exe connection.
func GenerateRDPFile(hostname string, ipAddress string) string {
	target := ipAddress
	if target == "" || target == "127.0.0.1" {
		target = hostname
	}

	return fmt.Sprintf(`full address:s:%s
prompt for credentials:i:1
administrative session:i:1
screen mode id:i:2
use multimon:i:0
desktopwidth:i:1920
desktopheight:i:1080
session bpp:i:32
compression:i:1
keyboardhook:i:2
audiomode:i:0
redirectprinters:i:0
redirectcomports:i:0
redirectsmartcards:i:0
redirectclipboard:i:1
redirectposdevices:i:0
displayconnectionbar:i:1
autoreconnection enabled:i:1
authentication level:i:2
negotiate security layer:i:1
remoteapplicationmode:i:0
alternate shell:s:
shell working directory:s:
gatewayhostname:s:
gatewayusagemethod:i:0
gatewaycredentialssource:i:0
gatewayprofileusagemethod:i:0
`, target)
}
