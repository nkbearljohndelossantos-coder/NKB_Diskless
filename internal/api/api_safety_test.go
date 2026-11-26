package api_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"nkb-diskless/internal/models"
)

// TestCheckpointA_RebootAndWOLPermanentlyBlocked verifies that all reboot, shutdown,
// logoff, lock, and WOL operations are rejected with HTTP 403 and return DISABLED_BY_SAFETY_POLICY.
func TestCheckpointA_RebootAndWOLPermanentlyBlocked(t *testing.T) {
	_, ts, db, auth, cleanup := setupTestServer(t)
	defer cleanup()

	adminToken := getAdminToken(t, db, auth)

	// Register a test client
	client := &models.Client{
		Hostname:   "SAFETY-PC-01",
		MACAddress: "00:AA:BB:CC:DD:01",
		Status:     "online",
	}
	if err := db.CreateClient(client); err != nil {
		t.Fatalf("failed to create test client: %v", err)
	}

	// 1. Direct reboot endpoint: POST /api/v1/clients/{id}/reboot -> HTTP 403 DISABLED_BY_SAFETY_POLICY
	t.Run("POST /api/v1/clients/{id}/reboot", func(t *testing.T) {
		req, _ := http.NewRequest("POST", ts.URL+"/api/v1/clients/"+client.ID+"/reboot", nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("reboot request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("expected HTTP 403 Forbidden for reboot, got: %d", resp.StatusCode)
		}
		body, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(body), "DISABLED_BY_SAFETY_POLICY") {
			t.Fatalf("expected body to contain DISABLED_BY_SAFETY_POLICY, got: %s", string(body))
		}
	})

	// 2. Direct Wake-on-LAN endpoint: POST /api/v1/workstations/{id}/wol -> HTTP 403 DISABLED_BY_SAFETY_POLICY
	t.Run("POST /api/v1/workstations/{id}/wol", func(t *testing.T) {
		req, _ := http.NewRequest("POST", ts.URL+"/api/v1/workstations/"+client.ID+"/wol", nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("WOL request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("expected HTTP 403 Forbidden for WOL, got: %d", resp.StatusCode)
		}
		body, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(body), "DISABLED_BY_SAFETY_POLICY") {
			t.Fatalf("expected body to contain DISABLED_BY_SAFETY_POLICY, got: %s", string(body))
		}
	})

	// 3. Prohibited Remote Commands via POST /api/v1/workstations/{id}/command:
	// shutdown, force_shutdown, reboot, restart, logoff, force_logoff, lock, lock_workstation, wol, wake_on_lan, wake-on-lan
	prohibitedCommands := []string{
		"shutdown",
		"force_shutdown",
		"reboot",
		"restart",
		"logoff",
		"force_logoff",
		"lock",
		"lock_workstation",
		"wol",
		"wake_on_lan",
		"wake-on-lan",
	}

	for _, cmd := range prohibitedCommands {
		t.Run("Command: "+cmd, func(t *testing.T) {
			payload, _ := json.Marshal(map[string]string{
				"command_type": cmd,
			})
			req, _ := http.NewRequest("POST", ts.URL+"/api/v1/workstations/"+client.ID+"/command", bytes.NewBuffer(payload))
			req.Header.Set("Authorization", "Bearer "+adminToken)
			req.Header.Set("Content-Type", "application/json")

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("command request failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusForbidden {
				t.Fatalf("expected HTTP 403 Forbidden for prohibited command '%s', got: %d", cmd, resp.StatusCode)
			}
			body, _ := io.ReadAll(resp.Body)
			if !strings.Contains(string(body), "DISABLED_BY_SAFETY_POLICY") {
				t.Fatalf("expected error body to contain DISABLED_BY_SAFETY_POLICY for '%s', got: %s", cmd, string(body))
			}
		})
	}
}

// TestCheckpointE_CSRFFailClosed verifies that state-changing requests authenticated via cookie
// that lack Sec-Fetch-Site, Origin, and Referer fail closed with HTTP 403.
func TestCheckpointE_CSRFFailClosed(t *testing.T) {
	_, ts, db, auth, cleanup := setupTestServer(t)
	defer cleanup()

	adminToken := getAdminToken(t, db, auth)

	// State-changing POST authenticated via cookie without Origin or Referer
	clientPayload := `{"mac_address":"00:11:22:33:44:88","hostname":"STRIPPED-CSRF"}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/clients", bytes.NewBufferString(clientPayload))
	req.Header.Set("Cookie", "nkb_session="+adminToken)
	req.Header.Set("Content-Type", "application/json")
	// Deliberately omit Sec-Fetch-Site, Origin, and Referer

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected HTTP 403 Forbidden when Origin and Referer are absent on cookie mutation, got: %d", resp.StatusCode)
	}
}

// TestHTTPBootIPXE_ValidationAndSafety tests the /boot/ipxe endpoint for input validation and Phase 3A safety.
func TestHTTPBootIPXE_ValidationAndSafety(t *testing.T) {
	_, ts, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	// 1. Missing MAC returns HTTP 400
	t.Run("Missing MAC", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/boot/ipxe")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected HTTP 400 for missing MAC, got: %d", resp.StatusCode)
		}
	})

	// 2. Malformed MAC returns HTTP 400
	malformedMACs := []string{
		"not-a-mac",
		"00:11:22:33:44",       // only 5 octets
		"00:11:22:33:44:55:66", // 7 octets
		"GG:HH:II:JJ:KK:LL",    // non-hex characters
		"'; DROP TABLE clients; --",
	}

	for _, badMAC := range malformedMACs {
		t.Run("Malformed MAC: "+badMAC, func(t *testing.T) {
			resp, err := http.Get(ts.URL + "/boot/ipxe?mac=" + badMAC)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("expected HTTP 400 for malformed MAC %q, got: %d", badMAC, resp.StatusCode)
			}
		})
	}

	// 3. Valid MAC returns HTTP 200 with Safe Phase 3A Script
	t.Run("Valid MAC Phase 3A Script", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/boot/ipxe?mac=00:AA:BB:CC:DD:EE&platform=efi&arch=x86_64")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected HTTP 200 for valid MAC, got: %d", resp.StatusCode)
		}

		contentType := resp.Header.Get("Content-Type")
		if !strings.HasPrefix(contentType, "text/plain") {
			t.Fatalf("expected Content-Type text/plain, got: %s", contentType)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("failed to read body: %v", err)
		}
		script := string(body)

		// Assert safe Phase 3A diagnostics are present
		if !strings.Contains(script, "NKB DISKLESS PLATFORM - PHASE 3A PXE TEST") {
			t.Errorf("script missing Phase 3A branding header")
		}
		if !strings.Contains(script, "00:AA:BB:CC:DD:EE") {
			t.Errorf("script missing normalized MAC address")
		}
		if !strings.Contains(script, "NO DISKS ATTACHED") {
			t.Errorf("script missing safe diagnostic disclaimer")
		}

		// STRICT SAFETY POLICY: Prohibit SAN boot and OS kernel commands in Phase 3A
		for _, prohibited := range []string{"sanboot", "sanhook", "iscsi:", "kernel", "initrd", "wimboot"} {
			if strings.Contains(strings.ToLower(script), prohibited) {
				t.Errorf("SAFETY VIOLATION: /boot/ipxe script contains prohibited command %q", prohibited)
			}
		}
	})
}

// TestHTTPBootHandler_Isolation verifies that BootHandler() strictly exposes ONLY /boot/ipxe
// and completely isolates administrative APIs and web dashboard routes from HTTP boot clients.
func TestHTTPBootHandler_Isolation(t *testing.T) {
	server, _, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	// Spin up isolated test server running ONLY the BootHandler
	bootTS := httptest.NewServer(server.BootHandler())
	defer bootTS.Close()

	// 1. /boot/ipxe MUST be accessible
	t.Run("Allow /boot/ipxe on BootHandler", func(t *testing.T) {
		resp, err := http.Get(bootTS.URL + "/boot/ipxe?mac=00:11:22:33:44:55")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected HTTP 200 on /boot/ipxe, got: %d", resp.StatusCode)
		}
	})

	// 2. All administrative /api/ endpoints MUST return 404 Not Found
	restrictedEndpoints := []struct {
		method string
		path   string
	}{
		{"GET", "/api/v1/system/overview"},
		{"GET", "/api/v1/workstations"},
		{"POST", "/api/v1/auth/login"},
		{"GET", "/api/v1/disks"},
		{"GET", "/api/v1/audit-logs"},
		{"GET", "/"},
	}

	for _, tc := range restrictedEndpoints {
		t.Run("Block "+tc.method+" "+tc.path, func(t *testing.T) {
			req, err := http.NewRequest(tc.method, bootTS.URL+tc.path, nil)
			if err != nil {
				t.Fatalf("failed to build request: %v", err)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusNotFound {
				t.Fatalf("SAFETY VIOLATION: expected HTTP 404 Not Found on BootHandler for %s %s, got: %d", tc.method, tc.path, resp.StatusCode)
			}
		})
	}
}

// TestHTTPBootHandler_MACValidation_And_Diagnostics tests MAC address parameter formats
// and verifies that Phase 3A diagnostic mode returns safe scripts with zero disk/SAN directives.
func TestHTTPBootHandler_MACValidation_And_Diagnostics(t *testing.T) {
	server, _, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	bootTS := httptest.NewServer(server.BootHandler())
	defer bootTS.Close()

	// 1. Valid MAC formats
	validMACs := []string{
		"00:11:22:33:44:55",
		"00-11-22-33-44-55",
		"001122334455",
		"aa:bb:cc:dd:ee:ff",
		"AA-BB-CC-DD-EE-FF",
	}
	for _, mac := range validMACs {
		t.Run("Valid MAC "+mac, func(t *testing.T) {
			resp, err := http.Get(bootTS.URL + "/boot/ipxe?mac=" + mac)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("expected HTTP 200 for valid MAC %q, got: %d", mac, resp.StatusCode)
			}

			body, _ := io.ReadAll(resp.Body)
			script := string(body)

			// Diagnostic mode checks
			if !strings.Contains(script, "PHASE 3A PXE TEST") {
				t.Errorf("expected script to contain Phase 3A diagnostic banner")
			}
			if !strings.Contains(script, "NO DISKS ATTACHED") {
				t.Errorf("expected script to contain safe diagnostic disclaimer")
			}
			// Strict prohibition checks
			for _, prohibited := range []string{"sanboot", "sanhook", "iscsi:", "kernel", "initrd", "wimboot"} {
				if strings.Contains(strings.ToLower(script), prohibited) {
					t.Fatalf("SAFETY VIOLATION: Diagnostic script contained prohibited directive %q", prohibited)
				}
			}
		})
	}

	// 2. Invalid MAC formats -> HTTP 400 Bad Request
	invalidMACs := []string{
		"",
		"invalid-mac",
		"00:11:22:33:44",
		"00:11:22:33:44:55:66",
		"00:GG:22:33:44:55",
		"12345",
		"'; DROP TABLE clients; --",
	}
	for _, mac := range invalidMACs {
		t.Run("Invalid MAC "+mac, func(t *testing.T) {
			url := bootTS.URL + "/boot/ipxe"
			if mac != "" {
				url += "?mac=" + mac
			}
			resp, err := http.Get(url)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("expected HTTP 400 Bad Request for invalid MAC %q, got: %d", mac, resp.StatusCode)
			}
		})
	}
}



