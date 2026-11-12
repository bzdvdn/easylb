// Package address builds the desired load balancer address list.
package address

import (
	"net/netip"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
)

// Options controls which node addresses are published.
type Options struct {
	// NodeIPTypes lists the node address types to publish, for example
	// NodeInternalIP. Only IP address types make sense here.
	NodeIPTypes []corev1.NodeAddressType
	// PublishNodeIPs disables node address publication when false.
	PublishNodeIPs bool
}

// Desired returns the sorted, de-duplicated list of load balancer ingress IPs
// for svc. Invalid VIP values are returned separately so the caller can emit an
// event without blocking publication of the valid addresses.
func Desired(svc *corev1.Service, nodes []*corev1.Node, slices []*discoveryv1.EndpointSlice, rawVIPs string, opts Options) (ips []string, invalidVIPs []string) {
	return DesiredWithVIPs(svc, nodes, slices, strings.Split(rawVIPs, ","), opts)
}

// DesiredWithVIPs is Desired for an already split VIP list.
func DesiredWithVIPs(svc *corev1.Service, nodes []*corev1.Node, slices []*discoveryv1.EndpointSlice, vips []string, opts Options) (ips []string, invalidVIPs []string) {
	want4, want6 := serviceFamilies(svc)
	seen := make(map[string]struct{})
	add := func(ip string) {
		if !familyAllowed(ip, want4, want6) {
			return
		}
		if _, ok := seen[ip]; ok {
			return
		}
		seen[ip] = struct{}{}
		ips = append(ips, ip)
	}

	if opts.PublishNodeIPs {
		for _, ip := range nodeIPs(svc, nodes, slices, opts) {
			add(ip)
		}
	}

	valid, invalid := parseVIPs(vips)
	for _, ip := range valid {
		add(ip)
	}
	sort.Strings(ips)
	return ips, invalid
}

func nodeIPs(svc *corev1.Service, nodes []*corev1.Node, slices []*discoveryv1.EndpointSlice, opts Options) []string {
	local := svc.Spec.ExternalTrafficPolicy == corev1.ServiceExternalTrafficPolicyTypeLocal
	var ready map[string]struct{}
	if local {
		ready = readyNodes(slices)
	}

	types := make(map[corev1.NodeAddressType]struct{}, len(opts.NodeIPTypes))
	for _, addressType := range opts.NodeIPTypes {
		types[addressType] = struct{}{}
	}

	var out []string
	for _, node := range nodes {
		if node == nil || !nodeReady(node) {
			continue
		}
		if local {
			if _, ok := ready[node.Name]; !ok {
				continue
			}
		}
		for _, addr := range node.Status.Addresses {
			if _, ok := types[addr.Type]; !ok {
				continue
			}
			out = append(out, addr.Address)
		}
	}
	return out
}

func readyNodes(slices []*discoveryv1.EndpointSlice) map[string]struct{} {
	ready := make(map[string]struct{})
	for _, slice := range slices {
		if slice == nil {
			continue
		}
		for _, endpoint := range slice.Endpoints {
			if endpoint.NodeName == nil {
				continue
			}
			if endpoint.Conditions.Ready == nil || !*endpoint.Conditions.Ready {
				continue
			}
			ready[*endpoint.NodeName] = struct{}{}
		}
	}
	return ready
}

func nodeReady(node *corev1.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

func parseVIPs(values []string) (valid, invalid []string) {
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		addr, err := netip.ParseAddr(value)
		if err != nil || addr.Zone() != "" {
			invalid = append(invalid, value)
			continue
		}
		valid = append(valid, addr.String())
	}
	return valid, invalid
}

func serviceFamilies(svc *corev1.Service) (want4, want6 bool) {
	for _, family := range svc.Spec.IPFamilies {
		switch family {
		case corev1.IPv4Protocol:
			want4 = true
		case corev1.IPv6Protocol:
			want6 = true
		}
	}
	if want4 || want6 {
		return want4, want6
	}
	for _, ip := range svc.Spec.ClusterIPs {
		if isIPv4(ip) {
			want4 = true
		}
		if isIPv6(ip) {
			want6 = true
		}
	}
	if !want4 && !want6 {
		want4 = true
	}
	return want4, want6
}

func familyAllowed(ip string, want4, want6 bool) bool {
	if isIPv4(ip) {
		return want4
	}
	if isIPv6(ip) {
		return want6
	}
	return false
}

func isIPv4(value string) bool {
	addr, err := netip.ParseAddr(value)
	return err == nil && addr.Is4()
}

func isIPv6(value string) bool {
	addr, err := netip.ParseAddr(value)
	return err == nil && addr.Is6() && !addr.Is4In6()
}
