package client_mgmt_test

import (
	"strings"
	"testing"

	"nkb-diskless/internal/services/client_mgmt"
)

func TestWakeOnLANPacketFormatting(t *testing.T) {
	// Test valid MAC formats
	err := client_mgmt.SendWakeOnLAN("00:1A:2B:3C:4D:5E", "127.0.0.1")
	if err != nil {
		t.Fatalf("failed to send WOL to localhost: %v", err)
	}

	errHyphen := client_mgmt.SendWakeOnLAN("00-1A-2B-3C-4D-5E", "127.0.0.1")
	if errHyphen != nil {
		t.Fatalf("failed to send hyphenated WOL: %v", errHyphen)
	}

	// Test invalid MAC
	errInvalid := client_mgmt.SendWakeOnLAN("INVALID_MAC", "127.0.0.1")
	if errInvalid == nil {
		t.Fatalf("expected error for invalid MAC, got nil")
	}
}

func TestRDPFileGeneration(t *testing.T) {
	rdp := client_mgmt.GenerateRDPFile("NKB-PC-01", "192.168.10.150")
	if !strings.Contains(rdp, "full address:s:192.168.10.150") {
		t.Errorf("expected RDP to contain target IP, got: %s", rdp)
	}
	if !strings.Contains(rdp, "prompt for credentials:i:1") {
		t.Errorf("expected prompt for credentials flag in RDP")
	}
}
