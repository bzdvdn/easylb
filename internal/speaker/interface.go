package speaker

import (
	"fmt"
	"net"
	"net/netip"
)

// InterfaceForIP returns the up, non-loopback interface that owns ip.
func InterfaceForIP(ip netip.Addr) (*net.Interface, error) {
	ip = ip.Unmap()

	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	for i := range interfaces {
		iface := interfaces[i]
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		hasIP, err := interfaceHasIP(&iface, ip)
		if err != nil {
			continue
		}
		if hasIP {
			return &iface, nil
		}
	}
	return nil, fmt.Errorf("no interface owns address %s", ip)
}

func interfaceHasIP(iface *net.Interface, ip netip.Addr) (bool, error) {
	addrs, err := iface.Addrs()
	if err != nil {
		return false, err
	}
	for _, addr := range addrs {
		var raw net.IP
		switch value := addr.(type) {
		case *net.IPNet:
			raw = value.IP
		case *net.IPAddr:
			raw = value.IP
		default:
			continue
		}
		parsed, ok := netip.AddrFromSlice(raw)
		if !ok {
			continue
		}
		if parsed.Unmap() == ip {
			return true, nil
		}
	}
	return false, nil
}
