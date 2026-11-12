// Package vip resolves the external VIPs configured for a Service.
package vip

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
)

// Mode describes who owns a VIP.
type Mode string

const (
	// ModeExternal means the VIP is managed outside easyLB, for example by
	// keepalived configured by a cluster administrator.
	ModeExternal Mode = "external"
	// ModeL2 means the easyLB speaker owns the VIP and advertises it via
	// ARP/NDP.
	ModeL2 Mode = "l2"
	// ModeBGP is reserved for a future easyLB BGP speaker.
	ModeBGP Mode = "bgp"
)

// Spec is a single VIP and its ownership mode.
type Spec struct {
	IP   string
	Mode Mode
}

// Source returns the VIPs configured for a Service.
type Source interface {
	VIPs(ctx context.Context, svc *corev1.Service) ([]Spec, error)
}

// dedupeSpecs rejects a VIP configured with conflicting ownership modes and
// removes duplicate identical entries. One VIP must have exactly one owner.
func dedupeSpecs(specs []Spec) ([]Spec, error) {
	seen := make(map[string]Mode, len(specs))
	out := make([]Spec, 0, len(specs))
	for _, spec := range specs {
		if mode, ok := seen[spec.IP]; ok {
			if mode != spec.Mode {
				return nil, fmt.Errorf("VIP %s is configured with conflicting modes %q and %q", spec.IP, mode, spec.Mode)
			}
			continue
		}
		seen[spec.IP] = spec.Mode
		out = append(out, spec)
	}
	return out, nil
}
