// Package ipam expands VIPPool address specifications and allocates addresses.
package ipam

import (
	"fmt"
	"net/netip"
	"sort"
	"strings"
)

// MaxPoolAddresses bounds the number of addresses a single pool may expand to.
const MaxPoolAddresses = 4096

// Expand parses a list of addresses, CIDRs and ranges into a sorted, unique
// address list.
func Expand(addresses []string) ([]netip.Addr, error) {
	seen := make(map[netip.Addr]struct{})
	out := make([]netip.Addr, 0, len(addresses))

	add := func(addr netip.Addr) error {
		addr = addr.Unmap()
		if _, ok := seen[addr]; ok {
			return nil
		}
		seen[addr] = struct{}{}
		out = append(out, addr)
		if len(out) > MaxPoolAddresses {
			return fmt.Errorf("pool expands to more than %d addresses", MaxPoolAddresses)
		}
		return nil
	}

	for _, raw := range addresses {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		switch {
		case strings.Contains(raw, "/"):
			prefix, err := netip.ParsePrefix(raw)
			if err != nil {
				return nil, fmt.Errorf("invalid CIDR %q: %w", raw, err)
			}
			prefix = prefix.Masked()
			for addr := prefix.Addr(); prefix.Contains(addr); addr = addr.Next() {
				if err := add(addr); err != nil {
					return nil, err
				}
				if !addr.IsValid() {
					break
				}
			}
		case strings.Contains(raw, "-"):
			parts := strings.SplitN(raw, "-", 2)
			start, err := netip.ParseAddr(strings.TrimSpace(parts[0]))
			if err != nil {
				return nil, fmt.Errorf("invalid range %q: %w", raw, err)
			}
			end, err := netip.ParseAddr(strings.TrimSpace(parts[1]))
			if err != nil {
				return nil, fmt.Errorf("invalid range %q: %w", raw, err)
			}
			if start.Is4() != end.Is4() {
				return nil, fmt.Errorf("range %q mixes address families", raw)
			}
			if start.Compare(end) > 0 {
				return nil, fmt.Errorf("range %q starts after it ends", raw)
			}
			for addr := start; addr.Compare(end) <= 0; addr = addr.Next() {
				if err := add(addr); err != nil {
					return nil, err
				}
				if !addr.IsValid() {
					break
				}
			}
		default:
			addr, err := netip.ParseAddr(raw)
			if err != nil {
				return nil, fmt.Errorf("invalid address %q: %w", raw, err)
			}
			if err := add(addr); err != nil {
				return nil, err
			}
		}
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].Compare(out[j]) < 0
	})
	return out, nil
}
