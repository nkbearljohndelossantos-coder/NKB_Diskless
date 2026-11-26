package network_mgmt

import (
	"fmt"
	"strings"
	"time"
)

// VerificationResult summarizes the live check of physical network configuration.
type VerificationResult struct {
	Verified        bool     `json:"verified"`
	Status          string   `json:"status"` // "PASS", "FAIL", "BLOCKED"
	InterfaceAlias  string   `json:"interface_alias"`
	ExpectedIP      string   `json:"expected_ip"`
	ActualIPs       []string `json:"actual_ips"`
	ExpectedGateway string   `json:"expected_gateway"`
	ActualGateways  []string `json:"actual_gateways"`
	Message         string   `json:"message"`
	Details         []string `json:"details"`
}

// StagingService handles the validation and verification of the network staging state.
type StagingService struct {
	configMgr *NetworkConfigManager
	discovery *DiscoveryService
}

// NewStagingService creates a new StagingService instance.
func NewStagingService(cfgMgr *NetworkConfigManager, discovery *DiscoveryService) *StagingService {
	return &StagingService{
		configMgr: cfgMgr,
		discovery: discovery,
	}
}

// VerifyLiveConfiguration inspects the actual operating system state against the profile.
func (s *StagingService) VerifyLiveConfiguration(profile *NetworkProfile) (*VerificationResult, error) {
	adapters, err := s.discovery.DiscoverAdapters()
	if err != nil {
		return nil, fmt.Errorf("failed to discover adapters during verification: %w", err)
	}

	res := &VerificationResult{
		InterfaceAlias:  profile.SelectedAdapter.Alias,
		ExpectedIP:      fmt.Sprintf("%s/%d", profile.IPv4Config.IPAddress, profile.IPv4Config.PrefixLength),
		ExpectedGateway: profile.IPv4Config.DefaultGateway,
		Details:         []string{},
	}

	_, matched := s.configMgr.ReconcileAdapter(profile, adapters)
	if matched == nil {
		res.Verified = false
		res.Status = StatusBlocked
		res.Message = fmt.Sprintf("Selected adapter '%s' (MAC: %s) is physically MISSING or disconnected.", profile.SelectedAdapter.Alias, profile.SelectedAdapter.MAC)
		profile.LastVerification = StatusBlocked
		profile.Status = StatusMissing
		now := time.Now().UTC()
		profile.LastVerifiedAt = &now
		_ = s.configMgr.SaveProfile(profile)
		return res, nil
	}

	res.ActualIPs = matched.IPv4Addresses
	res.ActualGateways = matched.DefaultGateways

	// 0. Protection Check: If the adapter is classified as PROTECTED_INTERNET due to an active host gateway
	if matched.SafetyRole == RoleProtectedInternet {
		res.Verified = false
		res.Status = StatusBlocked
		res.Message = fmt.Sprintf("Adapter '%s' is currently classified as PROTECTED_INTERNET (%s). Staging is BLOCKED until host default gateway is shifted to an alternate management adapter.",
			matched.InterfaceAlias, matched.SafetyWarning)
		profile.LastVerification = StatusBlocked
		profile.Status = StatusBlocked
		now := time.Now().UTC()
		profile.LastVerifiedAt = &now
		_ = s.configMgr.SaveProfile(profile)
		return res, nil
	}

	// 1. Check IP Address assignment
	expectedPrefix := fmt.Sprintf("%s/%d", profile.IPv4Config.IPAddress, profile.IPv4Config.PrefixLength)
	ipMatched := false
	for _, ip := range matched.IPv4Addresses {
		if strings.TrimSpace(ip) == expectedPrefix || strings.HasPrefix(ip, profile.IPv4Config.IPAddress+"/") || ip == profile.IPv4Config.IPAddress {
			ipMatched = true
			break
		}
	}

	if !ipMatched {
		res.Verified = false
		res.Status = "FAIL"
		res.Message = fmt.Sprintf("IP mismatch: expected %s, but adapter has %v", expectedPrefix, matched.IPv4Addresses)
		profile.LastVerification = "FAIL"
		now := time.Now().UTC()
		profile.LastVerifiedAt = &now
		_ = s.configMgr.SaveProfile(profile)
		return res, nil
	}
	res.Details = append(res.Details, fmt.Sprintf("IPv4 address verified: %s", expectedPrefix))

	// 2. Check Gateway (Isolated mode requires zero gateway)
	if profile.NetworkMode == ModeIsolatedStandalone {
		hasActiveGW := false
		for _, gw := range matched.DefaultGateways {
			cleanGW := strings.TrimSpace(gw)
			if cleanGW != "" && cleanGW != "0.0.0.0" {
				hasActiveGW = true
				break
			}
		}

		if hasActiveGW {
			res.Verified = false
			res.Status = "FAIL"
			res.Message = fmt.Sprintf("Gateway violation: isolated mode requires zero gateway, but found %v", matched.DefaultGateways)
			profile.LastVerification = "FAIL"
			now := time.Now().UTC()
			profile.LastVerifiedAt = &now
			_ = s.configMgr.SaveProfile(profile)
			return res, nil
		}
		res.Details = append(res.Details, "Air-gap isolation verified: Zero default gateway configured.")

		// Check DNS in isolated mode
		hasDNS := false
		for _, dns := range matched.DNSServers {
			if strings.TrimSpace(dns) != "" {
				hasDNS = true
				break
			}
		}
		if hasDNS {
			res.Verified = false
			res.Status = "FAIL"
			res.Message = fmt.Sprintf("DNS violation: isolated mode requires zero external DNS servers, but found %v", matched.DNSServers)
			profile.LastVerification = "FAIL"
			profile.Status = StatusError
			now := time.Now().UTC()
			profile.LastVerifiedAt = &now
			_ = s.configMgr.SaveProfile(profile)
			return res, nil
		}
		res.Details = append(res.Details, "Air-gap isolation verified: Zero DNS servers configured.")
	}

	res.Verified = true
	res.Status = "PASS"
	res.Message = fmt.Sprintf("Configuration 100%% verified on adapter '%s' (%s). Ready for PXE diagnostic test.", matched.InterfaceAlias, expectedPrefix)
	profile.LastVerification = "PASS"
	profile.Status = StatusStaged
	now := time.Now().UTC()
	profile.LastVerifiedAt = &now
	_ = s.configMgr.SaveProfile(profile)

	return res, nil
}
