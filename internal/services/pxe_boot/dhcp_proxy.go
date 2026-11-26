package pxe_boot

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"
)

// ProxyDHCPServer listens on UDP port 4011 to supply PXE boot parameters to clients
// without disrupting or interfering with existing corporate DHCP servers.
type ProxyDHCPServer struct {
	bindIP     net.IP
	serverIP   net.IP
	port       int
	httpPort   int
	conn       *net.UDPConn
	stopCh     chan struct{}
	wg         sync.WaitGroup
	recentReqs sync.Map // MAC+XID -> time.Time (deduplication)
}

// NewProxyDHCPServer constructs a new ProxyDHCP listener bound explicitly to bindIP.
func NewProxyDHCPServer(bindIP string, port int, httpPort int) (*ProxyDHCPServer, error) {
	if err := ValidateBindAddress(bindIP); err != nil {
		return nil, fmt.Errorf("invalid ProxyDHCP bind address: %w", err)
	}

	ip := net.ParseIP(bindIP).To4()
	if ip == nil {
		return nil, fmt.Errorf("ProxyDHCP requires an IPv4 address: %s", bindIP)
	}

	if port <= 0 || port > 65535 {
		port = 4011
	}
	if httpPort <= 0 || httpPort > 65535 {
		httpPort = 8080
	}

	return &ProxyDHCPServer{
		bindIP:   ip,
		serverIP: ip,
		port:     port,
		httpPort: httpPort,
		stopCh:   make(chan struct{}),
	}, nil
}

// Start launches the ProxyDHCP listener on UDP port 4011.
func (p *ProxyDHCPServer) Start() error {
	addr := &net.UDPAddr{
		Port: p.port,
		IP:   net.IPv4zero,
	}

	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		return fmt.Errorf("failed to bind ProxyDHCP on 0.0.0.0:%d: %w", p.port, err)
	}

	// Enable SO_BROADCAST
	if rawConn, err := conn.SyscallConn(); err == nil {
		_ = rawConn.Control(func(fd uintptr) {
			setBroadcast(fd)
		})
	}
	p.conn = conn

	slog.Info("NKB ProxyDHCP Service started", "bind_ip", p.bindIP.String(), "port", p.port, "http_port", p.httpPort)
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

// Architecture Constants according to RFC 4578 & IANA DHCP Processor Architecture Types
const (
	ArchBIOS    uint16 = 0      // Intel x86 BIOS
	ArchEFI32   uint16 = 6      // EFI IA32 (32-bit UEFI) -> Unsupported for 64-bit NKB!
	ArchUEFI64  uint16 = 7      // EFI Byte Code / EFI x86-64
	ArchEFIX64  uint16 = 9      // EFI x86-64
	ArchARM32   uint16 = 10     // ARM 32-bit UEFI
	ArchARM64   uint16 = 11     // ARM 64-bit UEFI
	ArchUnknown uint16 = 0xFFFF // Unspecified / Missing Option 93
)

// ParseOption77UserClasses parses DHCP Option 77 and returns all User Class strings.
// Supports sequential RFC 3004 chunks ([uc_len][uc_data]) and raw string fallbacks.
func ParseOption77UserClasses(options []byte) []string {
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

		// Option 77: User Class Identifier (RFC 3004)
		if opt == 77 && length > 0 {
			val := options[idx+2 : idx+2+length]
			var classes []string
			cIdx := 0
			isRFC3004 := true
			// Parse sequential RFC 3004 chunks: [uc_len (1 byte)] [uc_data (uc_len bytes)]
			for cIdx < len(val) {
				ucLen := int(val[cIdx])
				if ucLen > 0 && cIdx+1+ucLen <= len(val) {
					classes = append(classes, string(val[cIdx+1:cIdx+1+ucLen]))
					cIdx += 1 + ucLen
				} else {
					isRFC3004 = false
					break
				}
			}
			if isRFC3004 && len(classes) > 0 {
				return classes
			}
			// Fallback: raw string representation
			trimmed := strings.TrimSpace(string(val))
			if trimmed != "" {
				return []string{trimmed}
			}
			return nil
		}

		idx += 2 + length
	}
	return nil
}

// IsIPXEUserClass tests if any User Class entry is an exact, case-insensitive match for "iPXE".
// This prevents substring false positives (e.g., "anti-ipxe", "tripxe").
func IsIPXEUserClass(classes []string) bool {
	for _, c := range classes {
		tokens := strings.FieldsFunc(c, func(r rune) bool {
			return r == ' ' || r == '\t' || r == ',' || r == ';'
		})
		for _, token := range tokens {
			if strings.EqualFold(token, "iPXE") {
				return true
			}
		}
	}
	return false
}

// ParseOption77UserClass extracts the User Class string(s) (Option 77) from DHCP options payload.
// Maintained for backward compatibility.
func ParseOption77UserClass(options []byte) string {
	classes := ParseOption77UserClasses(options)
	return strings.Join(classes, " ")
}

// ParseClientArchList extracts all Client System Architecture Types (Option 93) from DHCP options payload.
// RFC 4578 specifies that Option 93 can contain a list of 16-bit architecture identifiers.
func ParseClientArchList(options []byte) []uint16 {
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
			var archs []uint16
			for i := 0; i+1 < length; i += 2 {
				arch := binary.BigEndian.Uint16(options[idx+2+i : idx+4+i])
				archs = append(archs, arch)
			}
			return archs
		}

		idx += 2 + length
	}
	return nil
}

// SelectClientArch selects the best supported architecture from Option 93.
// Returns the architecture and whether it is supported by the NKB diskless bootloader.
func SelectClientArch(options []byte) (uint16, bool) {
	archs := ParseClientArchList(options)
	if len(archs) == 0 {
		return ArchUnknown, false
	}

	// First pass: preference for UEFI x86-64 (Types 7, 9)
	for _, a := range archs {
		if a == ArchUEFI64 || a == ArchEFIX64 {
			return a, true
		}
	}

	// Second pass: Legacy BIOS (Type 0)
	for _, a := range archs {
		if a == ArchBIOS {
			return a, true
		}
	}

	// Unsupported architectures (e.g. Type 6 EFI IA32, Type 10 ARM32, Type 11 ARM64)
	return archs[0], false
}

// ParseClientArch extracts the Client System Architecture Type (Option 93) from DHCP options payload.
// Returns detected arch if supported or known, otherwise falls back to ArchUEFI64.
func ParseClientArch(options []byte) uint16 {
	arch, supported := SelectClientArch(options)
	if supported {
		return arch
	}
	if arch != ArchUnknown {
		return arch
	}
	return ArchUEFI64 // legacy fallback if Option 93 is completely missing
}

// DetermineBootFile selects the appropriate bootloader or chainloaded HTTP script URL
// based on DHCP Option 77 (User Class) and Option 93 (Client Arch).
// Does not incorrectly classify unsupported architectures as x64.
func DetermineBootFile(userClass string, arch uint16, serverIP string, httpPort int, clientMAC string) (bootFile string, isHTTP bool) {
	// If Option 77 contains valid iPXE token, client already executed first-stage iPXE
	classes := strings.Fields(userClass)
	if len(classes) == 0 && userClass != "" {
		classes = []string{userClass}
	}
	if IsIPXEUserClass(classes) {
		return fmt.Sprintf("http://%s:%d/boot/ipxe?mac=%s", serverIP, httpPort, clientMAC), true
	}

	// First-stage PXE ROM request: determine binary bootloader based on architecture
	switch arch {
	case ArchBIOS: // Type 0: Legacy BIOS
		return "bios/undionly.kpxe", false
	case ArchUEFI64, ArchEFIX64: // Type 7, 9: UEFI x86-64
		return "bootx64.efi", false
	case ArchEFI32: // Type 6: EFI IA32 (32-bit x86 UEFI) -> UNSUPPORTED for 64-bit NKB!
		return "", false
	default:
		// Unknown or unsupported architecture (ARM, etc.) -> DO NOT classify as x64!
		return "", false
	}
}

func (p *ProxyDHCPServer) handleDHCPRequest(remoteAddr *net.UDPAddr, req []byte) {
	if len(req) < 240 {
		return
	}

	options := req[240:]
	arch := ParseClientArch(options)
	userClass := ParseOption77UserClass(options)

	xid := req[4:8]
	chaddr := req[28:44] // Client MAC address in ethernet hardware address field

	macStr := fmt.Sprintf("%02X:%02X:%02X:%02X:%02X:%02X", chaddr[0], chaddr[1], chaddr[2], chaddr[3], chaddr[4], chaddr[5])

	// Duplicate request suppression within 1 second
	dedupKey := fmt.Sprintf("%s-%x", macStr, xid)
	if lastSeen, loaded := p.recentReqs.LoadOrStore(dedupKey, time.Now()); loaded {
		if time.Since(lastSeen.(time.Time)) < 1*time.Second {
			return
		}
		p.recentReqs.Store(dedupKey, time.Now())
	}

	bootFile, isHTTP := DetermineBootFile(userClass, arch, p.serverIP.String(), p.httpPort, macStr)
	if bootFile == "" {
		slog.Warn("ProxyDHCP: unsupported architecture or bootloader unavailable, ignoring request",
			"client_mac", macStr,
			"arch", arch,
		)
		return
	}

	slog.Info("ProxyDHCP PXE Request received",
		"client_mac", macStr,
		"arch", arch,
		"user_class", userClass,
		"is_ipxe_chainload", isHTTP,
		"assigned_bootloader", bootFile,
	)

	// Construct DHCP ACK / Proxy response (Opcode 2: BOOTREPLY)
	resp := make([]byte, 576+len(bootFile))
	resp[0] = 2 // BOOTREPLY
	resp[1] = 1 // Hardware type: Ethernet
	resp[2] = 6 // Hardware length: 6 bytes
	resp[3] = 0 // Hops
	copy(resp[4:8], xid)
	binary.BigEndian.PutUint16(resp[8:10], 0)        // Secs
	binary.BigEndian.PutUint16(resp[10:12], 0x8000) // Broadcast flag

	// Copy Client IP if present, otherwise Next-Server
	copy(resp[12:16], req[12:16]) // ciaddr
	copy(resp[20:24], p.serverIP) // siaddr (Next server IP)
	copy(resp[28:44], chaddr)

	// Bootfile name in legacy header field (offset 108, 128 bytes max)
	if len(bootFile) <= 128 {
		copy(resp[108:236], []byte(bootFile))
	} else {
		copy(resp[108:236], []byte(bootFile[:128]))
	}

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

	// Option 66: TFTP Server Name / Next-Server IP
	serverIPStr := []byte(p.serverIP.String())
	resp[optIdx] = 66
	resp[optIdx+1] = byte(len(serverIPStr))
	copy(resp[optIdx+2:], serverIPStr)
	optIdx += 2 + len(serverIPStr)

	// Option 67: Bootfile Name
	bootFileBytes := []byte(bootFile)
	if len(bootFileBytes) > 255 {
		bootFileBytes = bootFileBytes[:255]
	}
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
	// On Windows, if destination IP is 0.0.0.0 or unspecified, send as subnet/limited broadcast
	if destAddr.IP == nil || destAddr.IP.IsUnspecified() || destAddr.IP.Equal(net.IPv4zero) {
		destAddr.IP = net.IPv4bcast
	}

	bcastSubnet := net.IPv4(p.serverIP[0], p.serverIP[1], p.serverIP[2], 255)

	if p.conn != nil {
		if n, err := p.conn.WriteToUDP(resp[:optIdx], destAddr); err != nil {
			slog.Warn("ProxyDHCP reply failed", "dest", destAddr.String(), "err", err)
		} else {
			slog.Info("ProxyDHCP reply sent", "dest", destAddr.String(), "bytes", n)
		}
		// Also send subnet broadcast as fallback
		_, _ = p.conn.WriteToUDP(resp[:optIdx], &net.UDPAddr{IP: bcastSubnet, Port: destAddr.Port})
	}
}
