package pxe_boot

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"
)

// ProxyDHCPServer listens on UDP port 4011 to supply PXE boot parameters to clients
// without disrupting or interfering with existing corporate DHCP servers.
type ProxyDHCPServer struct {
	serverIP net.IP
	port     int
	conn     *net.UDPConn
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

// NewProxyDHCPServer constructs a new ProxyDHCP listener.
func NewProxyDHCPServer(serverIP string, port int) *ProxyDHCPServer {
	ip := net.ParseIP(serverIP)
	if ip == nil {
		ip = net.IPv4(127, 0, 0, 1)
	}
	return &ProxyDHCPServer{
		serverIP: ip.To4(),
		port:     port,
		stopCh:   make(chan struct{}),
	}
}

// Start launches the ProxyDHCP listener.
func (p *ProxyDHCPServer) Start() error {
	addr := &net.UDPAddr{
		Port: p.port,
		IP:   net.IPv4zero,
	}

	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		return fmt.Errorf("failed to bind ProxyDHCP on port %d: %w", p.port, err)
	}
	p.conn = conn

	slog.Info("NKB ProxyDHCP Service started", "port", p.port, "next_server_ip", p.serverIP.String())
	p.wg.Add(1)
	go p.listenLoop()
	return nil
}

// Stop cleanly terminates ProxyDHCP.
func (p *ProxyDHCPServer) Stop() {
	close(p.stopCh)
	if p.conn != nil {
		_ = p.conn.Close()
	}
	p.wg.Wait()
	slog.Info("NKB ProxyDHCP Service stopped")
}

func (p *ProxyDHCPServer) listenLoop() {
	defer p.wg.Done()
	buf := make([]byte, 1500)

	for {
		select {
		case <-p.stopCh:
			return
		default:
		}

		_ = p.conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		n, remoteAddr, err := p.conn.ReadFromUDP(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			select {
			case <-p.stopCh:
				return
			default:
				continue
			}
		}

		if n < 240 { // Minimum DHCP packet size
			continue
		}

		req := buf[:n]
		// Verify DHCP request opcode 1 (BOOTREQUEST)
		if req[0] != 1 {
			continue
		}

		// Inspect DHCP options (starting at offset 240, after magic cookie 0x63, 0x82, 0x53, 0x63)
		magicCookie := []byte{99, 130, 83, 99}
		if !bytes.Equal(req[236:240], magicCookie) {
			continue
		}

		go p.handleDHCPRequest(remoteAddr, req)
	}
}

func (p *ProxyDHCPServer) handleDHCPRequest(remoteAddr *net.UDPAddr, req []byte) {
	// Parse Client Architecture (Option 93: Client System Architecture Type)
	// 0 = Intel x86 PC (BIOS)
	// 6 = x86 EFI
	// 7 = x64 EFI (Modern UEFI x64)
	// 9 = EFI x86-64
	arch := p.parseClientArch(req[240:])
	bootFile := "uefi/ipxe.efi"
	if arch == 0 {
		bootFile = "bios/undionly.kpxe"
	}

	xid := req[4:8]
	chaddr := req[28:44] // Client MAC address in ethernet hardware address field

	macStr := fmt.Sprintf("%02X:%02X:%02X:%02X:%02X:%02X", chaddr[0], chaddr[1], chaddr[2], chaddr[3], chaddr[4], chaddr[5])
	slog.Info("ProxyDHCP PXE Request received", "client_mac", macStr, "arch", arch, "assigned_bootloader", bootFile)

	// Construct DHCP ACK / Proxy response (Opcode 2: BOOTREPLY)
	resp := make([]byte, 300+len(bootFile))
	resp[0] = 2 // BOOTREPLY
	resp[1] = 1 // Hardware type: Ethernet
	resp[2] = 6 // Hardware length: 6 bytes
	resp[3] = 0 // Hops
	copy(resp[4:8], xid)
	binary.BigEndian.PutUint16(resp[8:10], 0) // Secs
	binary.BigEndian.PutUint16(resp[10:12], 0x8000) // Broadcast flag

	// Copy Client IP if present, otherwise Next-Server
	copy(resp[12:16], req[12:16]) // ciaddr
	copy(resp[20:24], p.serverIP) // siaddr (Next server IP)
	copy(resp[28:44], chaddr)

	// Bootfile name in legacy header field (offset 108)
	copy(resp[108:236], []byte(bootFile))

	// Magic Cookie
	copy(resp[236:240], []byte{99, 130, 83, 99})

	// Options
	optIdx := 240
	// Option 53: DHCP Message Type = DHCPACK (5)
	resp[optIdx] = 53
	resp[optIdx+1] = 1
	resp[optIdx+2] = 5
	optIdx += 3

	// Option 54: Server Identifier
	resp[optIdx] = 54
	resp[optIdx+1] = 4
	copy(resp[optIdx+2:optIdx+6], p.serverIP)
	optIdx += 6

	// Option 60: Vendor Class Identifier = "PXEClient"
	vendorClass := []byte("PXEClient")
	resp[optIdx] = 60
	resp[optIdx+1] = byte(len(vendorClass))
	copy(resp[optIdx+2:], vendorClass)
	optIdx += 2 + len(vendorClass)

	// Option 66: TFTP Server Name
	serverIPStr := []byte(p.serverIP.String())
	resp[optIdx] = 66
	resp[optIdx+1] = byte(len(serverIPStr))
	copy(resp[optIdx+2:], serverIPStr)
	optIdx += 2 + len(serverIPStr)

	// Option 67: Bootfile Name
	bootFileBytes := []byte(bootFile)
	resp[optIdx] = 67
	resp[optIdx+1] = byte(len(bootFileBytes))
	copy(resp[optIdx+2:], bootFileBytes)
	optIdx += 2 + len(bootFileBytes)

	// End Option (255)
	resp[optIdx] = 255
	optIdx++

	// Send back to client on destination port
	destAddr := &net.UDPAddr{
		IP:   remoteAddr.IP,
		Port: remoteAddr.Port,
	}
	if destAddr.Port == 0 {
		destAddr.Port = 68
	}

	_, _ = p.conn.WriteToUDP(resp[:optIdx], destAddr)
}

func (p *ProxyDHCPServer) parseClientArch(options []byte) uint16 {
	idx := 0
	for idx < len(options) {
		opt := options[idx]
		if opt == 255 {
			break
		}
		if opt == 0 {
			idx++
			continue
		}
		if idx+1 >= len(options) {
			break
		}
		length := int(options[idx+1])
		if idx+2+length > len(options) {
			break
		}

		// Option 93: Client System Architecture Type
		if opt == 93 && length >= 2 {
			return binary.BigEndian.Uint16(options[idx+2 : idx+4])
		}

		idx += 2 + length
	}
	return 7 // Default to x64 EFI
}
