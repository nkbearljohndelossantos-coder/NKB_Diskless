package pxe_boot

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

var (
	ErrEmptyBindAddress      = errors.New("bind address cannot be empty")
	ErrWildcardBindProhibited = errors.New("wildcard bind address (0.0.0.0 or ::) is prohibited by NKB security policy; an explicit dedicated interface IP is required")
	ErrInvalidIPAddress      = errors.New("invalid IP address format")
)

// ValidateBindAddress validates that the provided IP string is a valid,
// non-wildcard, non-empty IPv4 address suitable for binding network boot services.
func ValidateBindAddress(ipStr string) error {
	trimmed := strings.TrimSpace(ipStr)
	if trimmed == "" {
		return ErrEmptyBindAddress
	}

	// Prohibit wildcard addresses
	if trimmed == "0.0.0.0" || trimmed == "::" || trimmed == "0:0:0:0:0:0:0:0" {
		return ErrWildcardBindProhibited
	}

	ip := net.ParseIP(trimmed)
	if ip == nil {
		return fmt.Errorf("%w: %q", ErrInvalidIPAddress, ipStr)
	}

	if ip.IsUnspecified() {
		return ErrWildcardBindProhibited
	}

	// Ensure IPv4
	if ip.To4() == nil {
		return fmt.Errorf("IPv6 binding is currently not supported for PXE/TFTP boot: %s", ipStr)
	}

	return nil
}
