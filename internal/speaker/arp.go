package speaker

import (
	"context"
	"net"
	"net/netip"
	"sync"

	"github.com/go-logr/logr"
	"github.com/mdlayher/arp"
)

var broadcastHardwareAddr = net.HardwareAddr{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}

type arpResponder struct {
	iface   *net.Interface
	vips    map[netip.Addr]struct{}
	client  *arp.Client
	writeMu sync.Mutex
}

func newARPResponder(iface *net.Interface, vips []netip.Addr) (*arpResponder, error) {
	client, err := arp.Dial(iface)
	if err != nil {
		return nil, err
	}
	addresses := make(map[netip.Addr]struct{}, len(vips))
	for _, vip := range vips {
		addresses[vip.Unmap()] = struct{}{}
	}
	return &arpResponder{
		iface:  iface,
		vips:   addresses,
		client: client,
	}, nil
}

// Run answers ARP requests for the configured VIPs and blocks until ctx is
// canceled or the ARP socket fails.
func (r *arpResponder) Run(ctx context.Context, logger logr.Logger) {
	defer func() { _ = r.client.Close() }()

	go func() {
		<-ctx.Done()
		_ = r.client.Close()
	}()

	for vip := range r.vips {
		r.gratuitous(vip, logger)
	}

	for {
		packet, _, err := r.client.Read()
		if err != nil {
			if ctx.Err() == nil {
				logger.Error(err, "ARP read failed", "interface", r.iface.Name)
			}
			return
		}
		if packet.Operation != arp.OperationRequest {
			continue
		}
		if _, ok := r.vips[packet.TargetIP.Unmap()]; !ok {
			continue
		}

		r.writeMu.Lock()
		err = r.client.Reply(packet, r.iface.HardwareAddr, packet.TargetIP)
		r.writeMu.Unlock()
		if err != nil {
			logger.V(1).Info("failed to send ARP reply", "interface", r.iface.Name, "error", err.Error())
		}
	}
}

func (r *arpResponder) gratuitous(vip netip.Addr, logger logr.Logger) {
	r.writeMu.Lock()
	defer r.writeMu.Unlock()

	request, err := arp.NewPacket(arp.OperationRequest, r.iface.HardwareAddr, vip, broadcastHardwareAddr, vip)
	if err == nil {
		if err := r.client.WriteTo(request, broadcastHardwareAddr); err != nil {
			logger.V(1).Info("failed to send gratuitous ARP request", "vip", vip.String(), "error", err.Error())
		}
	}

	reply, err := arp.NewPacket(arp.OperationReply, r.iface.HardwareAddr, vip, broadcastHardwareAddr, vip)
	if err == nil {
		if err := r.client.WriteTo(reply, broadcastHardwareAddr); err != nil {
			logger.V(1).Info("failed to send gratuitous ARP reply", "vip", vip.String(), "error", err.Error())
		}
	}
}
