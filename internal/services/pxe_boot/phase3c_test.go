package pxe_boot_test

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nkb-diskless/internal/services/pxe_boot"
)

// ============================================================================
// 1. DHCP OPTION 77 VALIDATION MATRIX
// ============================================================================

func TestDHCP_Option77_Validation_Matrix(t *testing.T) {
	serverIP := "192.168.50.1"
	httpPort := 8080
	clientMAC := "00:11:22:33:44:55"

	tests := []struct {
		name         string
		options      []byte
		expectIsIPXE bool
		expectHTTP   bool
	}{
		{
			name:         "Missing Option 77",
			options:      []byte{93, 2, 0, 7, 255}, // Only Option 93 present
			expectIsIPXE: false,
			expectHTTP:   false,
		},
		{
			name:         "Single User-Class (Raw string: 'iPXE')",
			options:      []byte{77, 4, 'i', 'P', 'X', 'E', 255},
			expectIsIPXE: true,
			expectHTTP:   true,
		},
		{
			name:         "Single User-Class (RFC 3004 chunk: [len=4, 'iPXE'])",
			options:      []byte{77, 5, 4, 'i', 'P', 'X', 'E', 255},
			expectIsIPXE: true,
			expectHTTP:   true,
		},
		{
			name:         "Single User-Class (Lower-case 'ipxe' case-insensitivity test)",
			options:      []byte{77, 5, 4, 'i', 'p', 'x', 'e', 255},
			expectIsIPXE: true,
			expectHTTP:   true,
		},
		{
			name:         "Single User-Class (Upper-case 'IPXE' case-insensitivity test)",
			options:      []byte{77, 5, 4, 'I', 'P', 'X', 'E', 255},
			expectIsIPXE: true,
			expectHTTP:   true,
		},
		{
			name: "Multiple User-Classes (RFC 3004: ['EngineeringLAN', 'iPXE'])",
			options: []byte{
				77, 20,
				14, 'E', 'n', 'g', 'i', 'n', 'e', 'e', 'r', 'i', 'n', 'g', 'L', 'A', 'N',
				4, 'i', 'P', 'X', 'E',
				255,
			},
			expectIsIPXE: true,
			expectHTTP:   true,
		},
		{
			name: "Multiple User-Classes (RFC 3004: ['iPXE', 'WorkstationV2'])",
			options: []byte{
				77, 19,
				4, 'i', 'P', 'X', 'E',
				13, 'W', 'o', 'r', 'k', 's', 't', 'a', 't', 'i', 'o', 'n', 'V', '2',
				255,
			},
			expectIsIPXE: true,
			expectHTTP:   true,
		},
		{
			name:         "Malformed Option 77 (Declared chunk length exceeds option length)",
			options:      []byte{77, 4, 15, 'A', 'B', 'C', 255}, // uc_len=15 but option len=4
			expectIsIPXE: false,
			expectHTTP:   false,
		},
		{
			name:         "Malformed Option 77 (Zero-length chunk)",
			options:      []byte{77, 4, 0, 'A', 'B', 'C', 255}, // uc_len=0
			expectIsIPXE: false,
			expectHTTP:   false,
		},
		{
			name:         "Unexpected User-Class ('pxebox' - not iPXE)",
			options:      []byte{77, 7, 6, 'p', 'x', 'e', 'b', 'o', 'x', 255},
			expectIsIPXE: false,
			expectHTTP:   false,
		},
		{
			name:         "Unexpected User-Class ('anti-ipxe-client' - substring should not match)",
			options:      []byte{77, 17, 16, 'a', 'n', 't', 'i', '-', 'i', 'p', 'x', 'e', '-', 'c', 'l', 'i', 'e', 'n', 't', 255},
			expectIsIPXE: false,
			expectHTTP:   false,
		},
		{
			name:         "Unexpected User-Class ('equipxenon' - substring should not match)",
			options:      []byte{77, 11, 10, 'e', 'q', 'u', 'i', 'p', 'x', 'e', 'n', 'o', 'n', 255},
			expectIsIPXE: false,
			expectHTTP:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			classes := pxe_boot.ParseOption77UserClasses(tc.options)
			isIPXE := pxe_boot.IsIPXEUserClass(classes)
			if isIPXE != tc.expectIsIPXE {
				t.Fatalf("IsIPXEUserClass(%v) = %v, expected %v", classes, isIPXE, tc.expectIsIPXE)
			}

			// Check boot file routing
			userClassStr := strings.Join(classes, " ")
			bootFile, isHTTP := pxe_boot.DetermineBootFile(userClassStr, pxe_boot.ArchUEFI64, serverIP, httpPort, clientMAC)
			if isHTTP != tc.expectHTTP {
				t.Fatalf("DetermineBootFile isHTTP = %v, expected %v (bootFile: %s)", isHTTP, tc.expectHTTP, bootFile)
			}

			if tc.expectHTTP {
				expectedURL := fmt.Sprintf("http://%s:%d/boot/ipxe?mac=%s", serverIP, httpPort, clientMAC)
				if bootFile != expectedURL {
					t.Fatalf("expected HTTP boot URL %q, got %q", expectedURL, bootFile)
				}
			} else {
				// Should receive first-stage TFTP bootloader
				if bootFile != "uefi/ipxe.efi" {
					t.Fatalf("expected first-stage 'uefi/ipxe.efi', got %q", bootFile)
				}
			}
		})
	}
}

// ============================================================================
// 2. DHCP OPTION 93 ARCHITECTURE VALIDATION MATRIX
// ============================================================================

func TestDHCP_Option93_Architecture_Matrix(t *testing.T) {
	serverIP := "192.168.50.1"
	httpPort := 8080
	clientMAC := "AA:BB:CC:DD:EE:FF"

	tests := []struct {
		name             string
		options          []byte
		expectArch       uint16
		expectSupported  bool
		expectedBootfile string
	}{
		{
			name:             "Type 0: Legacy BIOS",
			options:          []byte{93, 2, 0, 0, 255},
			expectArch:       pxe_boot.ArchBIOS,
			expectSupported:  true,
			expectedBootfile: "bios/undionly.kpxe",
		},
		{
			name:             "Type 6: EFI IA32 (32-bit UEFI) - MUST NOT BE CLASSIFIED AS X64",
			options:          []byte{93, 2, 0, 6, 255},
			expectArch:       pxe_boot.ArchEFI32,
			expectSupported:  false,
			expectedBootfile: "", // Empty: unsupported arch must not receive x64 bootloader
		},
		{
			name:             "Type 7: EFI x86-64 / EFI BC",
			options:          []byte{93, 2, 0, 7, 255},
			expectArch:       pxe_boot.ArchUEFI64,
			expectSupported:  true,
			expectedBootfile: "uefi/ipxe.efi",
		},
		{
			name:             "Type 9: EFI x86-64",
			options:          []byte{93, 2, 0, 9, 255},
			expectArch:       pxe_boot.ArchEFIX64,
			expectSupported:  true,
			expectedBootfile: "uefi/ipxe.efi",
		},
		{
			name:             "Type 10: ARM 32-bit UEFI (Unsupported)",
			options:          []byte{93, 2, 0, 10, 255},
			expectArch:       pxe_boot.ArchARM32,
			expectSupported:  false,
			expectedBootfile: "",
		},
		{
			name:             "Type 11: ARM 64-bit UEFI (Unsupported)",
			options:          []byte{93, 2, 0, 11, 255},
			expectArch:       pxe_boot.ArchARM64,
			expectSupported:  false,
			expectedBootfile: "",
		},
		{
			name:             "Type 999: Unknown architecture (Unsupported)",
			options:          []byte{93, 2, 0x03, 0xE7, 255},
			expectArch:       999,
			expectSupported:  false,
			expectedBootfile: "",
		},
		{
			name:             "Multiple Archs: [Type 6 (IA32), Type 7 (x64)] -> Selects Supported x64",
			options:          []byte{93, 4, 0, 6, 0, 7, 255},
			expectArch:       pxe_boot.ArchUEFI64,
			expectSupported:  true,
			expectedBootfile: "uefi/ipxe.efi",
		},
		{
			name:             "Multiple Archs: [Type 6 (IA32), Type 11 (ARM64)] -> Unsupported",
			options:          []byte{93, 4, 0, 6, 0, 11, 255},
			expectArch:       pxe_boot.ArchEFI32,
			expectSupported:  false,
			expectedBootfile: "",
		},
		{
			name:             "Option 93 Missing -> ArchUnknown",
			options:          []byte{53, 1, 1, 255},
			expectArch:       pxe_boot.ArchUnknown,
			expectSupported:  false,
			expectedBootfile: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			arch, supported := pxe_boot.SelectClientArch(tc.options)
			if arch != tc.expectArch {
				t.Fatalf("SelectClientArch arch = %d, expected %d", arch, tc.expectArch)
			}
			if supported != tc.expectSupported {
				t.Fatalf("SelectClientArch supported = %v, expected %v", supported, tc.expectSupported)
			}

			// Verify DetermineBootFile does not assign x64 bootloader to unsupported architectures
			bootFile, isHTTP := pxe_boot.DetermineBootFile("", arch, serverIP, httpPort, clientMAC)
			if isHTTP {
				t.Fatalf("expected isHTTP=false for first-stage PXE ROM")
			}
			if bootFile != tc.expectedBootfile {
				t.Fatalf("expected bootfile %q, got %q (arch=%d)", tc.expectedBootfile, bootFile, arch)
			}
		})
	}
}

// ============================================================================
// 3. STANDALONE DHCP AND PROXYDHCP CONSISTENCY
// ============================================================================

func TestDHCP_Standalone_And_Proxy_Consistency(t *testing.T) {
	// 1. Standalone DHCP Server constructor validation
	_, err := pxe_boot.NewStandaloneDHCPServer("0.0.0.0", 67, 8080, "192.168.50.100", "192.168.50.200")
	if err == nil {
		t.Fatalf("expected standalone DHCP to reject wildcard bind address 0.0.0.0")
	}

	srv, err := pxe_boot.NewStandaloneDHCPServer("192.168.50.1", 67, 8080, "192.168.50.100", "192.168.50.105")
	if err != nil {
		t.Fatalf("failed to construct standalone DHCP server: %v", err)
	}

	// 2. Client lease allocation
	mac1 := "00:11:22:33:44:01"
	ip1, err := srv.AllocateLease(mac1)
	if err != nil {
		t.Fatalf("failed to allocate lease for mac1: %v", err)
	}
	if ip1.String() != "192.168.50.100" {
		t.Fatalf("expected lease 192.168.50.100, got: %s", ip1.String())
	}

	// 2b. Same client requesting lease renewal gets same IP
	ip1Renew, err := srv.AllocateLease(mac1)
	if err != nil || !ip1Renew.Equal(ip1) {
		t.Fatalf("expected renewal to return same IP %s, got: %v", ip1, ip1Renew)
	}

	// 3. Lease pool sequential allocation
	for i := 2; i <= 6; i++ {
		mac := fmt.Sprintf("00:11:22:33:44:%02X", i)
		ip, err := srv.AllocateLease(mac)
		if err != nil {
			t.Fatalf("failed to allocate lease %d: %v", i, err)
		}
		expectedIP := fmt.Sprintf("192.168.50.%d", 99+i)
		if ip.String() != expectedIP {
			t.Fatalf("expected %s, got %s", expectedIP, ip.String())
		}
	}

	// 4. Lease pool exhaustion (pool size was 6: 100 to 105)
	macExhaust := "00:11:22:33:44:99"
	_, err = srv.AllocateLease(macExhaust)
	if err == nil {
		t.Fatalf("expected lease pool exhaustion error, but allocation succeeded")
	}

	// 5. ProxyDHCP Non-Allocation Behavior (Port 4011)
	proxySrv, err := pxe_boot.NewProxyDHCPServer("192.168.50.1", 4011, 8080)
	if err != nil {
		t.Fatalf("failed to construct ProxyDHCP server: %v", err)
	}

	// 6. ProxyDHCP Wildcard rejection
	_, err = pxe_boot.NewProxyDHCPServer("0.0.0.0", 4011, 8080)
	if err == nil {
		t.Fatalf("expected ProxyDHCP to reject wildcard 0.0.0.0")
	}

	// 7. Verify MessageType extraction
	optsDiscover := []byte{53, 1, 1, 255}
	if msgType := pxe_boot.ExtractMessageType(optsDiscover); msgType != 1 {
		t.Fatalf("expected DHCPDISCOVER (1), got: %d", msgType)
	}

	optsRequest := []byte{53, 1, 3, 255}
	if msgType := pxe_boot.ExtractMessageType(optsRequest); msgType != 3 {
		t.Fatalf("expected DHCPREQUEST (3), got: %d", msgType)
	}

	_ = proxySrv
}

// ============================================================================
// 4. TFTP PROTOCOL AND ACCESS VALIDATION
// ============================================================================

func TestTFTP_Protocol_Validation(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "nkb_tftp_proto_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create test bootloader file
	bootDir := filepath.Join(tempDir, "uefi")
	_ = os.MkdirAll(bootDir, 0755)
	testFile := filepath.Join(bootDir, "ipxe.efi")
	fileContent := make([]byte, 2048) // Multi-block 4x512 bytes
	for i := range fileContent {
		fileContent[i] = byte(i % 256)
	}
	_ = os.WriteFile(testFile, fileContent, 0644)

	// 1. Valid RRQ lookup
	resolved, err := pxe_boot.ValidateSafeTFTPPath(tempDir, "uefi/ipxe.efi")
	if err != nil {
		t.Fatalf("expected valid RRQ lookup to succeed, got: %v", err)
	}
	if resolved != testFile {
		t.Fatalf("expected %q, got: %q", testFile, resolved)
	}

	// 2. Traversal rejection
	invalidPaths := []string{
		"../uefi/ipxe.efi",
		"..\\uefi\\ipxe.efi",
		"uefi/../../../secret.txt",
		"C:\\Windows\\System32\\cmd.exe",
		"uefi/\x00hidden.efi",
		"/etc/passwd",
	}

	for _, p := range invalidPaths {
		_, err := pxe_boot.ValidateSafeTFTPPath(tempDir, p)
		if err == nil {
			t.Errorf("expected path %q to be rejected, but it succeeded", p)
		}
	}

	// 3. Missing file returns ErrNotExist
	_, err = pxe_boot.ValidateSafeTFTPPath(tempDir, "uefi/nonexistent.efi")
	if err != os.ErrNotExist {
		t.Fatalf("expected os.ErrNotExist, got: %v", err)
	}
}

// ============================================================================
// 5. BOOTLOADER STATIC VALIDATION
// ============================================================================

func TestBootloader_Static_Integrity(t *testing.T) {
	workspaceDir := filepath.Join("..", "..", "..")
	if _, err := os.Stat(filepath.Join(workspaceDir, "tftpboot")); os.IsNotExist(err) {
		workspaceDir = filepath.Join("..", "..")
	}

	// 1. BIOS bootloader verification
	biosPath := filepath.Join(workspaceDir, "tftpboot", "bios", "undionly.kpxe")
	fi, err := os.Stat(biosPath)
	if err != nil {
		t.Fatalf("BIOS bootloader missing at %s: %v", biosPath, err)
	}

	if fi.Size() != 80934 {
		t.Fatalf("BIOS bootloader size mismatch: expected 80934 bytes, got: %d", fi.Size())
	}

	bytesData, err := os.ReadFile(biosPath)
	if err != nil {
		t.Fatalf("failed to read BIOS bootloader: %v", err)
	}

	hash := sha256.Sum256(bytesData)
	hashStr := strings.ToUpper(fmt.Sprintf("%x", hash))
	expectedHash := "09B92D87ECE68558E9190E47A5DCB53ED5BDEA80A0FE725ABEB6F8D042362B42"
	if hashStr != expectedHash {
		t.Fatalf("BIOS bootloader SHA-256 mismatch!\nExpected: %s\nGot:      %s", expectedHash, hashStr)
	}

	// 2. Verify corrupted UEFI file is quarantined and recognized as HTML
	quarantinePath := filepath.Join(workspaceDir, "tftpboot", "uefi", "quarantine", "ipxe.efi.corrupted_404")
	if qData, err := os.ReadFile(quarantinePath); err == nil {
		if !strings.Contains(string(qData), "<html") && !strings.Contains(string(qData), "<!DOCTYPE") {
			t.Errorf("expected quarantined file to contain HTML error content")
		}
	}
}

// ============================================================================
// 6. HTTP BOOT SAFETY AND RESTRICTED ENDPOINT VALIDATION
// ============================================================================

func TestHTTPBoot_Safety_And_Isolation(t *testing.T) {
	// Diagnostic boot request
	diagReq := pxe_boot.BootRequest{
		MAC:         "00:11:22:33:44:55",
		IP:          "192.168.50.100",
		Platform:    "efi",
		Arch:        "x86_64",
		ServerIP:    "192.168.50.1",
		HTTPPort:    8080,
		Phase3AMode: true, // Strict Diagnostic Mode
	}

	script := pxe_boot.GeneratePhase3AScript(diagReq)

	// Verify absence of ALL dangerous / OS loading / storage directives
	forbidden := []string{
		"sanboot",
		"sanhook",
		"iscsi:",
		"kernel",
		"initrd",
		"wimboot",
		"bootmgr",
		"boot.sdi",
		"chain ",
		"login",
	}

	for _, directive := range forbidden {
		if strings.Contains(strings.ToLower(script), directive) {
			t.Fatalf("STRICT SAFETY VIOLATION: Diagnostic script contains forbidden directive %q:\n%s", directive, script)
		}
	}

	// Verify required diagnostic feedback
	if !strings.Contains(script, "00:11:22:33:44:55") {
		t.Fatalf("missing client MAC in script")
	}
	if !strings.Contains(script, "192.168.50.100") {
		t.Fatalf("missing client IP in script")
	}
	if !strings.Contains(script, "NO DISKS ATTACHED") {
		t.Fatalf("missing safe disclaimer in script")
	}
}
