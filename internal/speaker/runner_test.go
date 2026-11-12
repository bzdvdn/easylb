package speaker

import (
	"net/netip"
	"testing"
)

func TestAddressFamilies(t *testing.T) {
	vips := []netip.Addr{
		netip.MustParseAddr("192.168.56.240"),
		netip.MustParseAddr("2001:db8::10"),
	}
	if !hasIPv4(vips) {
		t.Fatal("expected IPv4 addresses")
	}
	if !hasIPv6(vips) {
		t.Fatal("expected IPv6 addresses")
	}
	if hasIPv4([]netip.Addr{netip.MustParseAddr("2001:db8::10")}) {
		t.Fatal("unexpected IPv4 address")
	}
	if hasIPv6([]netip.Addr{netip.MustParseAddr("192.168.56.240")}) {
		t.Fatal("unexpected IPv6 address")
	}
	if got := addressesToStrings(vips); len(got) != 2 || got[0] != "192.168.56.240" || got[1] != "2001:db8::10" {
		t.Fatalf("unexpected addresses: %v", got)
	}
}
