package pxe_boot

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// TFTPServer is an embedded high-performance TFTP service for PXE bootstrap files.
type TFTPServer struct {
	bindIP  net.IP
	rootDir string
	port    int
	conn    *net.UDPConn
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

// NewTFTPServer constructs a new TFTP listener bound explicitly to bindIP and pointing to rootDir.
func NewTFTPServer(bindIP string, rootDir string, port int) (*TFTPServer, error) {
	if err := ValidateBindAddress(bindIP); err != nil {
		return nil, fmt.Errorf("invalid TFTP bind address: %w", err)
	}

	ip := net.ParseIP(bindIP).To4()
	if ip == nil {
		return nil, fmt.Errorf("TFTP requires an IPv4 address: %s", bindIP)
	}

	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve TFTP root directory: %w", err)
	}

	if port <= 0 || port > 65535 {
		port = 69
	}

	return &TFTPServer{
		bindIP:  ip,
		rootDir: absRoot,
		port:    port,
		stopCh:  make(chan struct{}),
	}, nil
}

// Start begins listening on UDP port 69.
func (s *TFTPServer) Start() error {
	addr := &net.UDPAddr{
		Port: s.port,
		IP:   net.IPv4zero,
	}

	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		return fmt.Errorf("failed to bind TFTP on 0.0.0.0:%d: %w", s.port, err)
	}
	s.conn = conn

	slog.Info("NKB TFTP Service started", "bind_ip", s.bindIP.String(), "port", s.port, "root_dir", s.rootDir)

	s.wg.Add(1)
	go s.listenLoop()
	return nil
}

// Stop shuts down the TFTP server cleanly.
func (s *TFTPServer) Stop() {
	close(s.stopCh)
	if s.conn != nil {
		_ = s.conn.Close()
	}
	s.wg.Wait()
	slog.Info("NKB TFTP Service stopped")
}

func (s *TFTPServer) listenLoop() {
	defer s.wg.Done()
	buf := make([]byte, 1024)

	for {
		select {
		case <-s.stopCh:
			return
		default:
		}

		_ = s.conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		n, remoteAddr, err := s.conn.ReadFromUDP(buf)
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

		if n < 4 {
			continue
		}

		opcode := binary.BigEndian.Uint16(buf[0:2])
		// Opcode 1: RRQ (Read Request)
		if opcode == 1 {
			// Parse filename (must be null-terminated string per RFC 1350)
			if !bytes.Contains(buf[2:n], []byte{0}) {
				slog.Warn("TFTP malformed RRQ: missing null terminator", "client", remoteAddr.String())
				go s.sendError(remoteAddr, 4, "Illegal TFTP operation: malformed RRQ")
				continue
			}
			parts := bytes.Split(buf[2:n], []byte{0})
			filename := strings.TrimSpace(string(parts[0]))
			if filename == "" {
				slog.Warn("TFTP malformed RRQ: empty filename", "client", remoteAddr.String())
				go s.sendError(remoteAddr, 4, "Illegal TFTP operation: empty filename")
				continue
			}

			// Parse RFC 2347 / 2348 options (e.g. blksize, tsize)
			options := make(map[string]string)
			for i := 2; i+1 < len(parts); i += 2 {
				optName := strings.ToLower(strings.TrimSpace(string(parts[i])))
				optVal := strings.TrimSpace(string(parts[i+1]))
				if optName != "" {
					options[optName] = optVal
				}
			}

			slog.Info("TFTP Read Request received", "client", remoteAddr.String(), "file", filename, "options", options)
			go s.handleRRQ(remoteAddr, filename, options)
		} else if opcode == 2 {
			// Opcode 2: WRQ (Write Request) - STRICTLY PROHIBITED
			slog.Warn("TFTP Write Request rejected: server is read-only", "client", remoteAddr.String())
			go s.sendError(remoteAddr, 4, "Illegal TFTP operation: server is read-only")
		} else {
			// Unknown opcode
			slog.Warn("TFTP Unknown Opcode rejected", "opcode", opcode, "client", remoteAddr.String())
			go s.sendError(remoteAddr, 4, "Illegal TFTP operation: unknown opcode")
		}
	}
}

// ValidateSafeTFTPPath validates the requested file path to prevent directory traversal
// and verifies that the target resolves strictly within rootDir, evaluating symlinks & junctions.
// Returns the resolved file path or an error.
func ValidateSafeTFTPPath(rootDir, requestedFile string) (string, error) {
	if strings.Contains(requestedFile, "\x00") {
		return "", fmt.Errorf("path contains forbidden null byte")
	}

	// Normalize slashes
	raw := strings.ReplaceAll(requestedFile, "\\", "/")
	raw = strings.TrimPrefix(raw, "/")

	// Check for path traversal sequences or drive letters
	if strings.Contains(raw, "..") || strings.Contains(raw, ":") {
		return "", fmt.Errorf("directory traversal or invalid path format: %q", requestedFile)
	}

	cleaned := filepath.Clean(filepath.FromSlash(raw))
	if strings.HasPrefix(cleaned, "..") || cleaned == "." || filepath.IsAbs(cleaned) {
		return "", fmt.Errorf("path escapes root boundary: %q", requestedFile)
	}

	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return "", fmt.Errorf("invalid root dir: %w", err)
	}

	realRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		realRoot = absRoot
	}

	// Search in root, uefi/, bios/
	candidates := []string{
		filepath.Join(absRoot, cleaned),
		filepath.Join(absRoot, "uefi", cleaned),
		filepath.Join(absRoot, "bios", cleaned),
	}

	for _, cand := range candidates {
		absCand, err := filepath.Abs(cand)
		if err != nil {
			continue
		}
		// Strict lexical containment check
		rel, err := filepath.Rel(absRoot, absCand)
		if err != nil || strings.HasPrefix(rel, "..") || rel == ".." {
			return "", fmt.Errorf("path %q escapes root directory %q", absCand, absRoot)
		}

		if fi, err := os.Stat(absCand); err == nil && !fi.IsDir() {
			// Resolve symlinks/junctions to guarantee physical containment
			realCand, err := filepath.EvalSymlinks(absCand)
			if err != nil {
				return "", fmt.Errorf("failed to evaluate file symlink: %w", err)
			}
			realRel, err := filepath.Rel(realRoot, realCand)
			if err != nil || strings.HasPrefix(realRel, "..") || realRel == ".." {
				return "", fmt.Errorf("symlink target %q escapes physical root %q", realCand, realRoot)
			}
			return realCand, nil
		}
	}

	return "", os.ErrNotExist
}

func (s *TFTPServer) handleRRQ(clientAddr *net.UDPAddr, requestedFile string, options map[string]string) {
	targetPath, err := ValidateSafeTFTPPath(s.rootDir, requestedFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			slog.Warn("TFTP File Not Found", "requested", requestedFile, "client", clientAddr.String())
			s.sendError(clientAddr, 1, "File not found")
		} else {
			slog.Warn("TFTP Access Violation / Directory Traversal Blocked", "requested", requestedFile, "client", clientAddr.String(), "err", err)
			s.sendError(clientAddr, 2, "Access violation: path traversal prohibited")
		}
		return
	}

	file, err := os.Open(targetPath)
	if err != nil {
		s.sendError(clientAddr, 2, "Access violation: unable to open file")
		return
	}
	defer file.Close()

	fi, err := file.Stat()
	if err != nil {
		s.sendError(clientAddr, 2, "Unable to stat file")
		return
	}
	fileSize := fi.Size()

	// Negotiate block size and options (RFC 2347 / RFC 2348)
	blockSize := 512
	sendOACK := false
	var oackBuf bytes.Buffer
	oackBuf.Write([]byte{0x00, 0x06}) // Opcode 6: OACK

	if blkStr, ok := options["blksize"]; ok {
		if b, err := strconv.Atoi(blkStr); err == nil && b >= 512 && b <= 1468 {
			blockSize = b
			oackBuf.WriteString("blksize\x00")
			oackBuf.WriteString(strconv.Itoa(blockSize) + "\x00")
			sendOACK = true
		}
	}
	if _, ok := options["tsize"]; ok {
		oackBuf.WriteString("tsize\x00")
		oackBuf.WriteString(strconv.FormatInt(fileSize, 10) + "\x00")
		sendOACK = true
	}

	// Establish dedicated ephemeral socket bound strictly to bindIP for data transmission
	laddr := &net.UDPAddr{IP: s.bindIP, Port: 0}
	clientConn, err := net.DialUDP("udp4", laddr, clientAddr)
	if err != nil {
		slog.Error("TFTP DialUDP failed", "client", clientAddr.String(), "err", err)
		return
	}
	defer clientConn.Close()

	ackBuf := make([]byte, 16)

	// If client requested options, send OACK and wait for ACK 0
	if sendOACK {
		slog.Info("TFTP Option Negotiation (OACK)", "client", clientAddr.String(), "blksize", blockSize, "tsize", fileSize)
		oackBytes := oackBuf.Bytes()
		acknowledged := false
		for retries := 0; retries < 5; retries++ {
			if _, err := clientConn.Write(oackBytes); err != nil {
				return
			}
			_ = clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
			ackN, err := clientConn.Read(ackBuf)
			if err == nil && ackN >= 4 {
				ackOpcode := binary.BigEndian.Uint16(ackBuf[0:2])
				ackBlock := binary.BigEndian.Uint16(ackBuf[2:4])
				if ackOpcode == 4 && ackBlock == 0 {
					acknowledged = true
					break
				}
			}
		}
		if !acknowledged {
			slog.Warn("TFTP OACK not acknowledged by client, aborting transfer", "client", clientAddr.String())
			return
		}
	}

	var blockNum uint16 = 1
	dataBuf := make([]byte, blockSize)

	for {
		n, readErr := file.Read(dataBuf)
		if readErr != nil && readErr != io.EOF {
			return
		}

		// TFTP DATA packet: [0x00, 0x03, BlockHi, BlockLo, Data...]
		packet := make([]byte, 4+n)
		binary.BigEndian.PutUint16(packet[0:2], 3)
		binary.BigEndian.PutUint16(packet[2:4], blockNum)
		copy(packet[4:], dataBuf[:n])

		// Send with retry
		acknowledged := false
		for retries := 0; retries < 4; retries++ {
			_, err = clientConn.Write(packet)
			if err != nil {
				return
			}

			_ = clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
			ackN, err := clientConn.Read(ackBuf)
			if err == nil && ackN >= 4 {
				ackOpcode := binary.BigEndian.Uint16(ackBuf[0:2])
				ackBlock := binary.BigEndian.Uint16(ackBuf[2:4])
				if ackOpcode == 4 && ackBlock == blockNum {
					acknowledged = true
					break
				}
			}
		}

		if !acknowledged {
			slog.Warn("TFTP transmission timeout/aborted by client", "client", clientAddr.String(), "block", blockNum)
			return
		}

		if n < blockSize || readErr == io.EOF {
			// Final block sent and acknowledged
			slog.Info("TFTP Transfer completed successfully", "file", requestedFile, "client", clientAddr.String(), "total_blocks", blockNum, "block_size", blockSize)
			return
		}

		blockNum++
	}
}

func (s *TFTPServer) sendError(clientAddr *net.UDPAddr, errorCode uint16, msg string) {
	laddr := &net.UDPAddr{IP: s.bindIP, Port: 0}
	conn, err := net.DialUDP("udp4", laddr, clientAddr)
	if err != nil {
		return
	}
	defer conn.Close()

	packet := make([]byte, 5+len(msg))
	binary.BigEndian.PutUint16(packet[0:2], 5) // Opcode 5: ERROR
	binary.BigEndian.PutUint16(packet[2:4], errorCode)
	copy(packet[4:], msg)
	packet[len(packet)-1] = 0 // Null terminator

	_, _ = conn.Write(packet)
}
