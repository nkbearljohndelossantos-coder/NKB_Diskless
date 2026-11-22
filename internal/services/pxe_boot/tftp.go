package pxe_boot

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// TFTPServer is an embedded high-performance TFTP service for PXE bootstrap files.
type TFTPServer struct {
	rootDir string
	port    int
	conn    *net.UDPConn
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

// NewTFTPServer constructs a new TFTP listener pointing to rootDir.
func NewTFTPServer(rootDir string, port int) *TFTPServer {
	return &TFTPServer{
		rootDir: rootDir,
		port:    port,
		stopCh:  make(chan struct{}),
	}
}

// Start begins listening on the configured UDP port.
func (s *TFTPServer) Start() error {
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("0.0.0.0:%d", s.port))
	if err != nil {
		return err
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return fmt.Errorf("failed to bind TFTP on port %d: %w", s.port, err)
	}
	s.conn = conn

	slog.Info("NKB TFTP Service started", "port", s.port, "root_dir", s.rootDir)

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
			// Parse filename (null-terminated string)
			parts := bytes.Split(buf[2:n], []byte{0})
			if len(parts) < 1 {
				continue
			}
			filename := string(parts[0])
			slog.Debug("TFTP Read Request received", "client", remoteAddr.String(), "file", filename)
			go s.handleRRQ(remoteAddr, filename)
		}
	}
}

func (s *TFTPServer) handleRRQ(clientAddr *net.UDPAddr, requestedFile string) {
	// Clean and normalize file path to prevent directory traversal
	cleaned := filepath.Clean(strings.ReplaceAll(requestedFile, "\\", "/"))
	cleaned = strings.TrimPrefix(cleaned, "/")
	cleaned = strings.TrimPrefix(cleaned, "\\")

	// Search in root, uefi/, bios/
	candidates := []string{
		filepath.Join(s.rootDir, cleaned),
		filepath.Join(s.rootDir, "uefi", cleaned),
		filepath.Join(s.rootDir, "bios", cleaned),
	}

	var targetPath string
	for _, cand := range candidates {
		if fi, err := os.Stat(cand); err == nil && !fi.IsDir() {
			targetPath = cand
			break
		}
	}

	if targetPath == "" {
		slog.Warn("TFTP File Not Found", "requested", requestedFile, "client", clientAddr.String())
		s.sendError(clientAddr, 1, "File not found")
		return
	}

	file, err := os.Open(targetPath)
	if err != nil {
		s.sendError(clientAddr, 2, "Access violation")
		return
	}
	defer file.Close()

	// Establish dedicated ephemeral socket for data transmission
	clientConn, err := net.DialUDP("udp", nil, clientAddr)
	if err != nil {
		return
	}
	defer clientConn.Close()

	var blockNum uint16 = 1
	dataBuf := make([]byte, 512)
	ackBuf := make([]byte, 4)

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
		for retries := 0; retries < 3; retries++ {
			_, err = clientConn.Write(packet)
			if err != nil {
				return
			}

			_ = clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
			ackN, _, err := clientConn.ReadFrom(ackBuf)
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
			slog.Debug("TFTP transmission timeout/aborted by client", "client", clientAddr.String(), "block", blockNum)
			return
		}

		if n < 512 || readErr == io.EOF {
			// Final block sent and acknowledged
			slog.Info("TFTP Transfer completed", "file", requestedFile, "client", clientAddr.String(), "total_blocks", blockNum)
			return
		}

		blockNum++
	}
}

func (s *TFTPServer) sendError(clientAddr *net.UDPAddr, errorCode uint16, msg string) {
	conn, err := net.DialUDP("udp", nil, clientAddr)
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
