package pxe_boot_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nkb-diskless/internal/config"
	"nkb-diskless/internal/database"
	"nkb-diskless/internal/models"
	"nkb-diskless/internal/services/pxe_boot"
)

// TestDHCPProxy_Option77UserClass_Detection tests Option 77 detection and bootloader/HTTP routing.
func TestDHCPProxy_Option77UserClass_Detection(t *testing.T) {
	// 1. Raw string Option 77: [code=77, len=4, 'i', 'P', 'X', 'E']
	rawOpt77 := []byte{77, 4, 'i', 'P', 'X', 'E', 255}
	uc := pxe_boot.ParseOption77UserClass(rawOpt77)
	if uc != "iPXE" {
		t.Fatalf("expected 'iPXE', got: %q", uc)
	}

	// 2. RFC 3004 Chunk Option 77: [code=77, len=5, uc_len=4, 'i', 'P', 'X', 'E']
	rfcOpt77 := []byte{77, 5, 4, 'i', 'P', 'X', 'E', 255}
	ucRFC := pxe_boot.ParseOption77UserClass(rfcOpt77)
	if ucRFC != "iPXE" {
		t.Fatalf("expected 'iPXE' from RFC 3004 chunk, got: %q", ucRFC)
	}

	// 2b. Multi-class RFC 3004 Option 77: [code=77, len=12, uc1_len=6, "MyCorp", uc2_len=4, "iPXE"]
	multiOpt77 := []byte{77, 12, 6, 'M', 'y', 'C', 'o', 'r', 'p', 4, 'i', 'P', 'X', 'E', 255}
	ucMulti := pxe_boot.ParseOption77UserClass(multiOpt77)
	if !strings.Contains(ucMulti, "iPXE") || !strings.Contains(ucMulti, "MyCorp") {
		t.Fatalf("expected multi-class to contain both MyCorp and iPXE, got: %q", ucMulti)
	}

	// 2c. Malformed RFC 3004 chunk with declared length exceeding option bounds
	malformedOpt77 := []byte{77, 4, 10, 'A', 'B', 'C', 255}
	ucMalformed := pxe_boot.ParseOption77UserClass(malformedOpt77)
	if ucMalformed == "" {
		t.Fatalf("expected fallback on malformed chunk, got empty")
	}

	// 3. Client without Option 77
	noOpt77 := []byte{93, 2, 0, 7, 255}
	if ucNone := pxe_boot.ParseOption77UserClass(noOpt77); ucNone != "" {
		t.Fatalf("expected empty user class, got: %q", ucNone)
	}

	// 4. Test DetermineBootFile:
	// When iPXE client checks in (single or multi-class), it must be assigned the HTTP boot script
	bootFileMulti, isHTTPMulti := pxe_boot.DetermineBootFile(ucMulti, 7, "192.168.50.1", 8080, "00:11:22:33:44:55")
	if !isHTTPMulti || !strings.HasPrefix(bootFileMulti, "http://") {
		t.Fatalf("expected multi-class iPXE to trigger HTTP dynamic script, got: %q", bootFileMulti)
	}
	// When iPXE client checks in, it must be assigned the HTTP boot script
	bootFile, isHTTP := pxe_boot.DetermineBootFile("iPXE", 7, "192.168.50.1", 8080, "00:11:22:33:44:55")
	if !isHTTP {
		t.Fatalf("expected isHTTP=true for iPXE user class")
	}
	expectedURL := "http://192.168.50.1:8080/boot/ipxe?mac=00:11:22:33:44:55"
	if bootFile != expectedURL {
		t.Fatalf("expected %q, got: %q", expectedURL, bootFile)
	}

	// First-stage PXE client (UEFI x64, arch=7, no Option 77)
	bootFileUEFI, isHTTPUEFI := pxe_boot.DetermineBootFile("", 7, "192.168.50.1", 8080, "00:11:22:33:44:55")
	if isHTTPUEFI || bootFileUEFI != "uefi/ipxe.efi" {
		t.Fatalf("expected uefi/ipxe.efi, got %q (isHTTP=%v)", bootFileUEFI, isHTTPUEFI)
	}

	// First-stage PXE client (Legacy BIOS, arch=0, no Option 77)
	bootFileBIOS, isHTTPBIOS := pxe_boot.DetermineBootFile("", 0, "192.168.50.1", 8080, "00:11:22:33:44:55")
	if isHTTPBIOS || bootFileBIOS != "bios/undionly.kpxe" {
		t.Fatalf("expected bios/undionly.kpxe, got %q (isHTTP=%v)", bootFileBIOS, isHTTPBIOS)
	}
}

// TestDHCPProxy_Option93Arch_Detection tests architecture parsing for BIOS and UEFI types.
func TestDHCPProxy_Option93Arch_Detection(t *testing.T) {
	// Arch 0: BIOS
	optsBIOS := []byte{93, 2, 0, 0, 255}
	if arch := pxe_boot.ParseClientArch(optsBIOS); arch != 0 {
		t.Fatalf("expected arch 0, got %d", arch)
	}

	// Arch 7: UEFI x64
	optsUEFI7 := []byte{93, 2, 0, 7, 255}
	if arch := pxe_boot.ParseClientArch(optsUEFI7); arch != 7 {
		t.Fatalf("expected arch 7, got %d", arch)
	}

	// Arch 9: EFI x86-64
	optsUEFI9 := []byte{93, 2, 0, 9, 255}
	if arch := pxe_boot.ParseClientArch(optsUEFI9); arch != 9 {
		t.Fatalf("expected arch 9, got %d", arch)
	}

	// Malformed / Empty options defaults to UEFI (7)
	optsMalformed := []byte{93, 1, 0}
	if arch := pxe_boot.ParseClientArch(optsMalformed); arch != 7 {
		t.Fatalf("expected default arch 7 on malformed input, got %d", arch)
	}
}

// TestTFTP_DirectoryTraversalProtection tests directory traversal defense in TFTP path validation.
func TestTFTP_DirectoryTraversalProtection(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "nkb_tftp_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create valid structure
	uefiDir := filepath.Join(tempDir, "uefi")
	_ = os.MkdirAll(uefiDir, 0755)
	validFile := filepath.Join(uefiDir, "testboot.efi")
	_ = os.WriteFile(validFile, []byte("valid efi binary"), 0644)

	// 1. Valid file lookup in uefi subfolder
	target, err := pxe_boot.ValidateSafeTFTPPath(tempDir, "testboot.efi")
	if err != nil {
		t.Fatalf("expected valid lookup to succeed, got: %v", err)
	}
	if target != validFile {
		t.Fatalf("expected target %q, got: %q", validFile, target)
	}

	// 2. Traversal attempt with ../
	traversalPayloads := []string{
		"../secret.txt",
		"..\\secret.txt",
		"../../windows/win.ini",
		"uefi/../../../../boot.ini",
		"/etc/passwd",
		"C:\\Windows\\System32\\cmd.exe",
		"uefi/..\x00../windows",
	}

	for _, payload := range traversalPayloads {
		_, err := pxe_boot.ValidateSafeTFTPPath(tempDir, payload)
		if err == nil {
			t.Errorf("expected traversal payload %q to be rejected, but it succeeded", payload)
		}
	}

	// 3. Non-existent legitimate file returns ErrNotExist
	_, err = pxe_boot.ValidateSafeTFTPPath(tempDir, "nonexistent.efi")
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected ErrNotExist, got: %v", err)
	}
}

// TestNetworkBinding_Validation tests pure interface binding validation.
func TestNetworkBinding_Validation(t *testing.T) {
	// Rejection tests
	invalidAddrs := []string{
		"",
		"   ",
		"0.0.0.0",
		"::",
		"0:0:0:0:0:0:0:0",
		"999.999.999.999",
		"localhost",
		"2001:0db8:85a3:0000:0000:8a2e:0370:7334",
	}

	for _, addr := range invalidAddrs {
		if err := pxe_boot.ValidateBindAddress(addr); err == nil {
			t.Errorf("expected bind address %q to be rejected, but it passed", addr)
		}
	}

	// Acceptance tests
	validAddrs := []string{
		"192.168.50.1",
		"10.0.0.1",
		"127.0.0.1",
		"172.16.10.5",
	}

	for _, addr := range validAddrs {
		if err := pxe_boot.ValidateBindAddress(addr); err != nil {
			t.Errorf("expected valid bind address %q to pass, got: %v", addr, err)
		}
	}

	// Test constructors enforce validation
	_, err := pxe_boot.NewProxyDHCPServer("0.0.0.0", 4011, 8080)
	if err == nil {
		t.Fatalf("expected NewProxyDHCPServer with 0.0.0.0 to fail")
	}

	_, err = pxe_boot.NewTFTPServer("0.0.0.0", "C:\\NKB-PoC\\tftpboot", 69)
	if err == nil {
		t.Fatalf("expected NewTFTPServer with 0.0.0.0 to fail")
	}
}

// TestPhase3A_SafeScriptGeneration tests the strict safety restrictions of Phase 3A scripts.
func TestPhase3A_SafeScriptGeneration(t *testing.T) {
	req := pxe_boot.BootRequest{
		MAC:         "00:11:22:33:44:55",
		IP:          "192.168.50.100",
		Platform:    "efi",
		Arch:        "x86_64",
		ServerIP:    "192.168.50.1",
		HTTPPort:    8080,
		Phase3AMode: true,
	}

	script := pxe_boot.GeneratePhase3AScript(req)

	// 1. Mandatory Branding and Diagnostics
	if !strings.Contains(script, "NKB DISKLESS PLATFORM - PHASE 3A PXE TEST") {
		t.Errorf("missing Phase 3A branding header")
	}
	if !strings.Contains(script, "00:11:22:33:44:55") {
		t.Errorf("missing client MAC address")
	}
	if !strings.Contains(script, "192.168.50.100") {
		t.Errorf("missing client IP address")
	}
	if !strings.Contains(script, "NO DISKS ATTACHED") {
		t.Errorf("missing safe diagnostic disclaimer")
	}

	// 2. STRICT PROHIBITION CHECKS: Zero SAN / Storage / OS commands
	prohibitedDirectives := []string{
		"sanboot",
		"sanhook",
		"iscsi:",
		"kernel",
		"initrd",
		"wimboot",
		"bootmgr",
		"boot.sdi",
	}

	for _, directive := range prohibitedDirectives {
		if strings.Contains(strings.ToLower(script), directive) {
			t.Errorf("SAFETY VIOLATION: Phase 3A script contains prohibited directive %q", directive)
		}
	}

	// 3. Command options must only be prompt, shell, or reboot
	if !strings.Contains(script, "shell") || !strings.Contains(script, "reboot") {
		t.Errorf("expected script to contain safe shell and reboot handlers")
	}
}

// TestConfig_Validation_FailClosed tests configuration safety validation.
func TestConfig_Validation_FailClosed(t *testing.T) {
	cfg := config.DefaultConfig("")

	// Default state: Fail closed
	if cfg.EnableProxyDHCP || cfg.EnableTFTP {
		t.Fatalf("expected network boot services to be disabled by default")
	}

	// Enabling services on 0.0.0.0 must fail validation
	cfg.EnableProxyDHCP = true
	cfg.BindAddress = "0.0.0.0"
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected validation failure when EnableProxyDHCP=true with BindAddress=0.0.0.0")
	}

	// Setting explicit dedicated IP must pass validation
	cfg.BindAddress = "192.168.50.1"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected validation success with valid dedicated IP, got: %v", err)
	}
}

func TestDynamicScriptGeneration(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "nkb_pxe_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cfg := config.DefaultConfig(tempDir)
	cfg.DBDriver = "sqlite"

	db, err := database.Connect(cfg)
	if err != nil {
		t.Fatalf("failed to connect to db: %v", err)
	}
	defer db.Close()

	// Find migrations folder relative to workspace
	migrationsDir := filepath.Join("..", "..", "database", "migrations")
	if _, err := os.Stat(migrationsDir); os.IsNotExist(err) {
		migrationsDir = filepath.Join("..", "database", "migrations")
	}
	if _, err := os.Stat(migrationsDir); os.IsNotExist(err) {
		cwd, _ := os.Getwd()
		t.Logf("cwd: %s", cwd)
	}
	err = db.Migrate(migrationsDir)
	if err != nil {
		t.Fatalf("failed to migrate db in test: %v", err)
	}

	// Create a test disk
	disk := &models.VirtualDisk{
		Name:       "Production Line Windows 10 Image",
		FilePath:   filepath.Join(tempDir, "disks", "prod10.vhdx"),
		SizeBytes:  50000000000,
		DiskFormat: "VHDX",
		IsGolden:   true,
	}
	_ = db.CreateDisk(disk)

	// Create test client
	client := &models.Client{
		MACAddress:     "AA:BB:CC:DD:EE:FF",
		IPAddress:      "192.168.1.50",
		Hostname:       "LINE-01-STATION",
		AssignedDiskID: &disk.ID,
		Status:         "online",
		BootMode:       "UEFI",
		WriteCacheMode: "volatile",
	}
	_ = db.CreateClient(client)

	req := pxe_boot.BootRequest{
		MAC:         "AA:BB:CC:DD:EE:FF",
		IP:          "192.168.1.50",
		Platform:    "efi",
		Arch:        "x86_64",
		ServerIP:    "192.168.1.10",
		HTTPPort:    8080,
		ISCSIPort:   3260,
		ISCSIPrefix: "iqn.2026-11.com.nkb",
		Phase3AMode: false, // Testing normal boot generation
	}

	// 1. Test Normal Boot Script (when Phase3AMode = false)
	script, err := pxe_boot.GenerateScript(req, db)
	if err != nil {
		t.Fatalf("failed to generate script: %v", err)
	}
	if !strings.Contains(script, "LINE-01-STATION") {
		t.Errorf("expected script to contain hostname 'LINE-01-STATION'")
	}
	if !strings.Contains(script, "sanboot") || !strings.Contains(script, "iqn.2026-11.com.nkb:client-aabbccddeeff") {
		t.Errorf("expected script to contain proper sanboot command with client target IQN")
	}

	// 2. Test Super-Client Mode Script
	_ = db.SetSuperClient(client.ID, true, 60)
	superScript, err := pxe_boot.GenerateScript(req, db)
	if err != nil {
		t.Fatalf("failed to generate super client script: %v", err)
	}
	if !strings.Contains(superScript, "SUPER-CLIENT MAINTENANCE MODE") {
		t.Errorf("expected super client banner in script")
	}
	if !strings.Contains(superScript, "iqn.2026-11.com.nkb:superclient-aabbccddeeff") {
		t.Errorf("expected super client target IQN in script")
	}

	// 3. Test Phase 3A Safe Mode Override
	req.Phase3AMode = true
	safeScript, err := pxe_boot.GenerateScript(req, db)
	if err != nil {
		t.Fatalf("failed to generate safe script: %v", err)
	}
	if !strings.Contains(safeScript, "PHASE 3A PXE TEST") {
		t.Errorf("expected safe Phase 3A banner")
	}
	if strings.Contains(safeScript, "sanboot") {
		t.Errorf("SAFETY VIOLATION: safe script contains sanboot")
	}

	// 4. Test Unregistered Computer Script (with Phase3AMode = false)
	unregisteredReq := pxe_boot.BootRequest{
		MAC:         "11:22:33:44:55:66",
		IP:          "192.168.1.99",
		Platform:    "efi",
		Arch:        "x86_64",
		ServerIP:    "192.168.1.10",
		HTTPPort:    8080,
		ISCSIPort:   3260,
		ISCSIPrefix: "iqn.2026-11.com.nkb",
		Phase3AMode: false,
	}
	unregScript, err := pxe_boot.GenerateScript(unregisteredReq, db)
	if err != nil {
		t.Fatalf("failed to generate unreg script: %v", err)
	}
	if !strings.Contains(unregScript, "UNREGISTERED WORKSTATION") {
		t.Errorf("expected unregistered station prompt in script")
	}
}

// TestNetworkInterfaceDiscovery_Readonly verifies read-only network discovery and safety classification.
func TestNetworkInterfaceDiscovery_Readonly(t *testing.T) {
	ifaces, err := pxe_boot.DiscoverNetworkInterfaces()
	if err != nil {
		t.Fatalf("interface discovery failed: %v", err)
	}

	if len(ifaces) == 0 {
		t.Fatalf("expected at least one network interface on host")
	}

	for _, iface := range ifaces {
		if iface.Name == "" {
			t.Errorf("expected interface to have a name")
		}
		// If an interface has a gateway or active production IP, it must be marked as protected
		if iface.Gateway != "" && !iface.IsDedicatedTestNIC {
			if !iface.IsProtected {
				t.Errorf("interface %s has gateway %s but was not marked as protected", iface.Name, iface.Gateway)
			}
		}
	}
}

