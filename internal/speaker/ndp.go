package speaker

import (
	"context"
	"net"
	"net/netip"
	"sync"

	"github.com/go-logr/logr"
	"github.com/mdlayher/ndp"
	"golang.org/x/net/ipv6"

	"github.com/bzdvdn/easylb/internal/metrics"
)

type ndpResponder struct {
	iface   *net.Interface
	vips    map[netip.Addr]struct{}
	conn    *ndp.Conn
	writeMu sync.Mutex
}

func newNDPResponder(iface *net.Interface, vips []netip.Addr) (*ndpResponder, error) {
	conn, _, err := ndp.Listen(iface, ndp.LinkLocal)
	if err != nil {
		return nil, err
	}

	addresses := make(map[netip.Addr]struct{}, len(vips))
	for _, vip := range vips {
		if !vip.Is6() {
			continue
		}
		vip = vip.Unmap()
		addresses[vip] = struct{}{}
		group, err := ndp.SolicitedNodeMulticast(vip)
		if err != nil {
			_ = conn.Close()
			return nil, err
		}
		if err := conn.JoinGroup(group); err != nil {
			_ = conn.Close()
			return nil, err
		}
	}

	filter := &ipv6.ICMPFilter{}
	filter.SetAll(true)
	filter.Accept(ipv6.ICMPTypeNeighborSolicitation)
	if err := conn.SetICMPFilter(filter); err != nil {
		_ = conn.Close()
		return nil, err
	}

	return &ndpResponder{
		iface: iface,
		vips:  addresses,
		conn:  conn,
	}, nil
}

// Run answers IPv6 Neighbor Solicitations for the configured VIPs and blocks
// until ctx is canceled or the NDP socket fails.
func (r *ndpResponder) Run(ctx context.Context, logger logr.Logger) {
	defer func() { _ = r.conn.Close() }()

	go func() {
		<-ctx.Done()
		_ = r.conn.Close()
	}()

	for vip := range r.vips {
		r.gratuitous(vip, logger)
	}

	for {
		msg, _, from, err := r.conn.ReadFrom()
		if err != nil {
			if ctx.Err() == nil {
				metrics.SpeakerErrors.WithLabelValues(r.iface.Name, "ndp", "read").Inc()
				logger.Error(err, "NDP read failed", "interface", r.iface.Name)
			}
			return
		}
		ns, ok := msg.(*ndp.NeighborSolicitation)
		if !ok {
			continue
		}
		target := ns.TargetAddress
		if _, ok := r.vips[target]; !ok {
			continue
		}

		solicited := !from.IsUnspecified()
		dst := from
		if !solicited {
			dst = netip.MustParseAddr("ff02::1")
		}
		r.writeMu.Lock()
		err = r.conn.WriteTo(&ndp.NeighborAdvertisement{
			Solicited:     solicited,
			Override:      true,
			TargetAddress: target,
			Options: []ndp.Option{&ndp.LinkLayerAddress{
				Direction: ndp.Target,
				Addr:      r.iface.HardwareAddr,
			}},
		}, &ipv6.ControlMessage{HopLimit: ndp.HopLimit}, dst)
		r.writeMu.Unlock()
		if err != nil {
			metrics.SpeakerErrors.WithLabelValues(r.iface.Name, "ndp", "reply").Inc()
			logger.V(1).Info("failed to send NDP advertisement", "interface", r.iface.Name, "error", err.Error())
		}
	}
}

func (r *ndpResponder) gratuitous(vip netip.Addr, logger logr.Logger) {
	allNodes := netip.MustParseAddr("ff02::1")

	r.writeMu.Lock()
	defer r.writeMu.Unlock()

	err := r.conn.WriteTo(&ndp.NeighborAdvertisement{
		Solicited:     false,
		Override:      true,
		TargetAddress: vip,
		Options: []ndp.Option{&ndp.LinkLayerAddress{
			Direction: ndp.Target,
			Addr:      r.iface.HardwareAddr,
		}},
	}, &ipv6.ControlMessage{HopLimit: ndp.HopLimit}, allNodes)
	if err != nil {
		metrics.SpeakerErrors.WithLabelValues(r.iface.Name, "ndp", "write").Inc()
		logger.V(1).Info("failed to send unsolicited NDP advertisement", "vip", vip.String(), "error", err.Error())
	}
}
