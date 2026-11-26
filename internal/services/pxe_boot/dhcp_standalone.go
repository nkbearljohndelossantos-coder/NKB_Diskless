package pxe_boot

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"syscall"
	"time"
)

// DHCPLease represents an allocated dynamic IP lease.
type DHCPLease struct {
	MAC       string
	IP        net.IP
	ExpiresAt time.Time
}

// StandaloneDHCPServer provides a fully isolated, RFC 2131-compliant standalone DHCP server
// listening on UDP port 67 for air-gapped testbeds without an external DHCP router.
type StandaloneDHCPServer struct {
	bindIP      net.IP
	serverIP    net.IP
	port        int
	httpPort    int
	subnetMask  net.IPMask
	leaseStart  net.IP
	leaseEnd    net.IP
	leaseTime   time.Duration
	leases      map[string]*DHCPLease // MAC -> Lease
	leaseMutex  sync.Mutex
	conn        *net.UDPConn
	sendConn    *net.UDPConn
	stopCh      chan struct{}
	wg          sync.WaitGroup
	recentReqs  sync.Map // MAC+XID -> time.Time (deduplication)
}

// NewStandaloneDHCPServer constructs a new Standalone DHCP server bound strictly to bindIP on port 67.
func NewStandaloneDHCPServer(bindIP string, port int, httpPort int, leaseStartStr, leaseEndStr string) (*StandaloneDHCPServer, error) {
	if err := ValidateBindAddress(bindIP); err != nil {
		return nil, fmt.Errorf("invalid standalone DHCP bind address: %w", err)
	}

	ip := net.ParseIP(bindIP).To4()
	if ip == nil {
		return nil, fmt.Errorf("standalone DHCP requires an IPv4 address: %s", bindIP)
	}

	if port <= 0 || port > 65535 {
		port = 67
	}
	if httpPort <= 0 || httpPort > 65535 {
		httpPort = 8080
	}

	startIP := net.ParseIP(leaseStartStr).To4()
	if startIP == nil {
		startIP = net.IPv4(ip[0], ip[1], ip[2], 100)
	}

	endIP := net.ParseIP(leaseEndStr).To4()
	if endIP == nil {
		endIP = net.IPv4(ip[0], ip[1], ip[2], 200)
	}

	return &StandaloneDHCPServer{
		bindIP:     ip,
		serverIP:   ip,
		port:       port,
		httpPort:   httpPort,
		subnetMask: net.CIDRMask(24, 32), // 255.255.255.0
		leaseStart: startIP,
		leaseEnd:   endIP,
		leaseTime:  24 * time.Hour,
		leases:     make(map[string]*DHCPLease),
		stopCh:     make(chan struct{}),
	}, nil
}

// Start launches the standalone DHCP listener on UDP port 67.
func (s *StandaloneDHCPServer) Start() error {
	lc := net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			return c.Control(func(fd uintptr) {
				_ = syscall.SetsockoptInt(syscall.Handle(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
				_ = syscall.SetsockoptInt(syscall.Handle(fd), syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1)
			})
		},
	}

	// 1. Primary listener on 0.0.0.0:67 to receive broadcast DISCOVER packets
	conn, err := lc.ListenPacket(context.Background(), "udp4", fmt.Sprintf("0.0.0.0:%d", s.port))
	if err != nil {
		return fmt.Errorf("failed to bind standalone DHCP on 0.0.0.0:%d: %w", s.port, err)
	}
	s.conn = conn.(*net.UDPConn)

	// 2. Dedicated physical sender bound to s.bindIP:67 to guarantee replies exit the Realtek GbE adapter
	sendConn, err := lc.ListenPacket(context.Background(), "udp4", fmt.Sprintf("%s:%d", s.bindIP.String(), s.port))
	if err == nil {
		s.sendConn = sendConn.(*net.UDPConn)
		slog.Info("NKB Standalone DHCP physical interface sender bound", "interface_ip", s.bindIP.String(), "port", s.port)
	} else {
		slog.Warn("Dedicated physical sender bind fallback to default socket", "err", err)
	}

	slog.Info("NKB Standalone DHCP Service started",
		"bind_ip", s.bindIP.String(),
		"port", s.port,
		"lease_range", fmt.Sprintf("%s - %s", s.leaseStart.String(), s.leaseEnd.String()),
	)

	s.wg.Add(1)
	go s.listenLoopConn(s.conn)
	if s.sendConn != nil {
		s.wg.Add(1)
		go s.listenLoopConn(s.sendConn)
	}
	return nil
}

// Stop cleanly terminates the standalone DHCP server.
func (s *StandaloneDHCPServer) Stop() {
	close(s.stopCh)
	if s.conn != nil {
		_ = s.conn.Close()
	}
	if s.sendConn != nil {
		_ = s.sendConn.Close()
	}
	s.wg.Wait()
	slog.Info("NKB Standalone DHCP Service stopped")
}

func (s *StandaloneDHCPServer) listenLoopConn(c *net.UDPConn) {
	defer s.wg.Done()
	buf := make([]byte, 1500)

	for {
		select {
		case <-s.stopCh:
			return
		default:
		}

		_ = c.SetReadDeadline(time.Now().Add(1 * time.Second))
		n, remoteAddr, err := c.ReadFromUDP(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			select {
			case <-s.stopCh:
				return
			default:
				continue
			}
		}

		if n < 240 {
			continue
		}

		req := make([]byte, n)
		copy(req, buf[:n])
		if req[0] != 1 { // BOOTREQUEST only
			continue
		}

		// Magic Cookie check [99, 130, 83, 99]
		magicCookie := []byte{99, 130, 83, 99}
		if !bytes.Equal(req[236:240], magicCookie) {
			continue
		}

		go s.handlePacket(remoteAddr, req)
	}
}

// AllocateLease allocates or extends an IP lease for a given MAC address.
func (s *StandaloneDHCPServer) AllocateLease(mac string) (net.IP, error) {
	s.leaseMutex.Lock()
	defer s.leaseMutex.Unlock()

	now := time.Now()

	// 1. Existing unexpired or recently expired lease for this MAC
	if lease, exists := s.leases[mac]; exists {
		lease.ExpiresAt = now.Add(s.leaseTime)
		return lease.IP, nil
	}

	// 2. Find next available IP in range
	startVal := binary.BigEndian.Uint32(s.leaseStart.To4())
	endVal := binary.BigEndian.Uint32(s.leaseEnd.To4())

	usedIPs := make(map[uint32]bool)
	for m, l := range s.leases {
		if now.Before(l.ExpiresAt) {
			usedIPs[binary.BigEndian.Uint32(l.IP.To4())] = true
		} else {
			delete(s.leases, m)
		}
	}

	for ipVal := startVal; ipVal <= endVal; ipVal++ {
		if !usedIPs[ipVal] {
			ipBytes := make([]byte, 4)
			binary.BigEndian.PutUint32(ipBytes, ipVal)
			allocated := net.IP(ipBytes)
			s.leases[mac] = &DHCPLease{
				MAC:       mac,
				IP:        allocated,
				ExpiresAt: now.Add(s.leaseTime),
			}
			return allocated, nil
		}
	}

	return nil, fmt.Errorf("DHCP lease pool exhausted (%s - %s)", s.leaseStart.String(), s.leaseEnd.String())
}

// ExtractMessageType parses DHCP Option 53 (DHCP Message Type).
func ExtractMessageType(options []byte) byte {
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
		if opt == 53 && length >= 1 {
			return options[idx+2]
		}
		idx += 2 + length
	}
	return 0
}

func (s *StandaloneDHCPServer) handlePacket(remoteAddr *net.UDPAddr, req []byte) {
	if len(req) < 240 {
		return
	}

	options := req[240:]
	msgType := ExtractMessageType(options)
	if msgType == 0 {
		return
	}

	chaddr := req[28:44]
	macStr := fmt.Sprintf("%02X:%02X:%02X:%02X:%02X:%02X", chaddr[0], chaddr[1], chaddr[2], chaddr[3], chaddr[4], chaddr[5])
	xid := req[4:8]

	// Duplicate DHCP request deduplication within 1 second
	dedupKey := fmt.Sprintf("%s-%x-%d", macStr, xid, msgType)
	if lastSeen, loaded := s.recentReqs.LoadOrStore(dedupKey, time.Now()); loaded {
		if time.Since(lastSeen.(time.Time)) < 1*time.Second {
			// Duplicate request in-flight, suppress duplicate reply
			return
		}
		s.recentReqs.Store(dedupKey, time.Now())
	}

	arch, _ := SelectClientArch(options)
	userClasses := ParseOption77UserClasses(options)
	userClassStr := strings.Join(userClasses, " ")

	bootFile, isHTTP := DetermineBootFile(userClassStr, arch, s.serverIP.String(), s.httpPort, macStr)
	if bootFile == "" {
		bootFile = "bootx64.efi"
	}

	assignedIP, err := s.AllocateLease(macStr)
	if err != nil {
		slog.Error("DHCP lease allocation failed", "mac", macStr, "err", err)
		return
	}

	var respMsgType byte
	switch msgType {
	case 1: // DHCPDISCOVER -> DHCPOFFER
		respMsgType = 2
	case 3: // DHCPREQUEST -> DHCPACK
		respMsgType = 5
	default:
		// Standalone server ignores INFORM, DECLINE, RELEASE in PoC diagnostic mode
		return
	}

	slog.Info("Standalone DHCP Processing",
		"msg_in", msgType,
		"msg_out", respMsgType,
		"client_mac", macStr,
		"assigned_ip", assignedIP.String(),
		"boot_file", bootFile,
		"is_http", isHTTP,
	)

	resp := make([]byte, 576+len(bootFile))
	resp[0] = 2 // BOOTREPLY
	resp[1] = 1 // Ethernet
	resp[2] = 6 // Hardware address length: 6
	resp[3] = 0 // Hops
	copy(resp[4:8], xid)
	binary.BigEndian.PutUint16(resp[8:10], 0)        // Secs
	binary.BigEndian.PutUint16(resp[10:12], 0x8000) // Broadcast flag

	copy(resp[12:16], req[12:16])       // ciaddr
	copy(resp[16:20], assignedIP.To4()) // yiaddr (Your IP - Allocated Lease)
	copy(resp[20:24], s.serverIP.To4()) // siaddr (Next server IP / TFTP Server IP)
	copy(resp[28:44], chaddr)           // chaddr

	// sname: Server host name (bytes 44:108) - Left empty (all zeroes per RFC 2131)
	// Putting an IP string in sname causes UEFI firmware to attempt DNS resolution and freeze.

	// Bootfile name in legacy header field (128 bytes max, null terminated)
	if len(bootFile) <= 128 {
		copy(resp[108:236], []byte(bootFile))
	} else {
		copy(resp[108:236], []byte(bootFile[:128]))
	}

	// Magic Cookie
	copy(resp[236:240], []byte{99, 130, 83, 99})

	ip4 := s.serverIP.To4()
	var bcastSubnet net.IP
	if ip4 != nil {
		bcastSubnet = net.IPv4(ip4[0], ip4[1], ip4[2], 255)
	} else {
		bcastSubnet = net.IPv4bcast
	}

	optIdx := 240

	// Option 53: DHCP Message Type (DHCPOFFER=2, DHCPACK=5)
	resp[optIdx] = 53
	resp[optIdx+1] = 1
	resp[optIdx+2] = respMsgType
	optIdx += 3

	// Option 54: Server Identifier
	resp[optIdx] = 54
	resp[optIdx+1] = 4
	copy(resp[optIdx+2:optIdx+6], s.serverIP.To4())
	optIdx += 6

	// Option 1: Subnet Mask (255.255.255.0)
	resp[optIdx] = 1
	resp[optIdx+1] = 4
	copy(resp[optIdx+2:optIdx+6], []byte(s.subnetMask))
	optIdx += 6

	// Option 3: Router / Default Gateway (192.168.254.254)
	routerIP := net.ParseIP("192.168.254.254").To4()
	if routerIP == nil {
		routerIP = s.serverIP.To4()
	}
	resp[optIdx] = 3
	resp[optIdx+1] = 4
	copy(resp[optIdx+2:optIdx+6], routerIP)
	optIdx += 6

	// Option 6: DNS Servers (Router + Google 8.8.8.8)
	resp[optIdx] = 6
	resp[optIdx+1] = 8
	copy(resp[optIdx+2:optIdx+6], routerIP)
	copy(resp[optIdx+6:optIdx+10], net.ParseIP("8.8.8.8").To4())
	optIdx += 10

	// Option 28: Broadcast Address
	resp[optIdx] = 28
	resp[optIdx+1] = 4
	copy(resp[optIdx+2:optIdx+6], bcastSubnet.To4())
	optIdx += 6

	// Option 51: Lease Time (86400 seconds / 24 hours)
	resp[optIdx] = 51
	resp[optIdx+1] = 4
	binary.BigEndian.PutUint32(resp[optIdx+2:optIdx+6], 86400)
	optIdx += 6

	// Option 58: Renewal (T1) Time Value: 43200 seconds (12 hours)
	resp[optIdx] = 58
	resp[optIdx+1] = 4
	binary.BigEndian.PutUint32(resp[optIdx+2:optIdx+6], 43200)
	optIdx += 6

	// Option 59: Rebinding (T2) Time Value: 75600 seconds
	resp[optIdx] = 59
	resp[optIdx+1] = 4
	binary.BigEndian.PutUint32(resp[optIdx+2:optIdx+6], 75600)
	optIdx += 6

	// Option 66: TFTP Server Name / Next-Server IP
	serverIPStr := []byte(s.serverIP.String())
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

	bcastAddr := &net.UDPAddr{IP: net.IPv4bcast, Port: 68}
	subnetBcastAddr := &net.UDPAddr{IP: bcastSubnet, Port: 68}

	// 1. Send via dedicated physical adapter socket (forces transmission out of Realtek GbE)
	if s.sendConn != nil {
		if n, err := s.sendConn.WriteToUDP(resp[:optIdx], bcastAddr); err != nil {
			slog.Warn("Physical adapter broadcast to 255.255.255.255 failed", "err", err)
		} else {
			slog.Info("DHCP reply sent via Realtek Ethernet (255.255.255.255:68)", "mac", macStr, "ip", assignedIP.String(), "bytes", n)
		}
		_, _ = s.sendConn.WriteToUDP(resp[:optIdx], subnetBcastAddr)
		if assignedIP != nil && !assignedIP.IsUnspecified() {
			_, _ = s.sendConn.WriteToUDP(resp[:optIdx], &net.UDPAddr{IP: assignedIP, Port: 68})
		}
	}

	// 2. Also send via primary socket as redundant fallback
	if s.conn != nil {
		_, _ = s.conn.WriteToUDP(resp[:optIdx], bcastAddr)
		_, _ = s.conn.WriteToUDP(resp[:optIdx], subnetBcastAddr)
		if assignedIP != nil && !assignedIP.IsUnspecified() {
			_, _ = s.conn.WriteToUDP(resp[:optIdx], &net.UDPAddr{IP: assignedIP, Port: 68})
		}
	}
}
