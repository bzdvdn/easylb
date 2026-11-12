package ipam

import (
	"net/netip"
	"reflect"
	"testing"
)

func TestExpandListRangeAndCIDR(t *testing.T) {
	got, err := Expand([]string{
		"192.168.56.240-192.168.56.242",
		"192.168.56.241",
		"192.168.56.240/30",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []netip.Addr{
		netip.MustParseAddr("192.168.56.240"),
		netip.MustParseAddr("192.168.56.241"),
		netip.MustParseAddr("192.168.56.242"),
		netip.MustParseAddr("192.168.56.243"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected addresses: got %v, want %v", got, want)
	}
}

func TestExpandRejectsInvalidValues(t *testing.T) {
	if _, err := Expand([]string{"192.168.56.250-192.168.56.240"}); err == nil {
		t.Fatal("expected error for an inverted range")
	}
	if _, err := Expand([]string{"10.0.0.1-2001:db8::1"}); err == nil {
		t.Fatal("expected error for a mixed-family range")
	}
	if _, err := Expand([]string{"not-an-ip"}); err == nil {
		t.Fatal("expected error for an invalid address")
	}
}
