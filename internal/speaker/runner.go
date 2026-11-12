package speaker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"

	"github.com/bzdvdn/easylb/internal/metrics"
)

type runner struct {
	cancel    context.CancelFunc
	done      chan struct{}
	ifaceName string
	vips      []netip.Addr
	iface     *net.Interface
}

func (r *runner) matches(vips []netip.Addr, iface *net.Interface) bool {
	if r.ifaceName != iface.Name || len(r.vips) != len(vips) {
		return false
	}
	want := make(map[netip.Addr]struct{}, len(vips))
	for _, vip := range vips {
		want[vip] = struct{}{}
	}
	for _, vip := range r.vips {
		if _, ok := want[vip]; !ok {
			return false
		}
	}
	return true
}

func (r *runner) stop() {
	r.cancel()
	<-r.done
}

func startRunner(
	parent context.Context,
	key types.NamespacedName,
	vips []netip.Addr,
	iface *net.Interface,
	kubeClient kubernetes.Interface,
	leaseNamespace string,
	nodeName string,
	identity string,
	leaseDuration time.Duration,
	renewDeadline time.Duration,
	retryPeriod time.Duration,
	logger logr.Logger,
) *runner {
	ctx, cancel := context.WithCancel(parent)
	r := &runner{
		cancel:    cancel,
		done:      make(chan struct{}),
		ifaceName: iface.Name,
		vips:      append([]netip.Addr(nil), vips...),
		iface:     iface,
	}

	go func() {
		defer close(r.done)

		lock := &resourcelock.LeaseLock{
			LeaseMeta: metav1.ObjectMeta{
				Name:      leaseName(key),
				Namespace: leaseNamespace,
			},
			Client: kubeClient.CoordinationV1(),
			LockConfig: resourcelock.ResourceLockConfig{
				Identity: identity,
			},
		}

		elector, err := leaderelection.NewLeaderElector(leaderelection.LeaderElectionConfig{
			Lock:            lock,
			LeaseDuration:   leaseDuration,
			RenewDeadline:   renewDeadline,
			RetryPeriod:     retryPeriod,
			ReleaseOnCancel: true,
			Name:            "easylb-speaker",
			Callbacks: leaderelection.LeaderCallbacks{
				OnStartedLeading: func(ctx context.Context) {
					binding := key.Namespace + "/" + key.Name
					metrics.SpeakerLeaderChanges.WithLabelValues(nodeName).Inc()
					metrics.SpeakerAdvertisedVIPs.WithLabelValues(nodeName, binding).Set(float64(len(vips)))
					runResponders(ctx, iface, vips, logger)
				},
				OnStoppedLeading: func() {
					binding := key.Namespace + "/" + key.Name
					metrics.SpeakerAdvertisedVIPs.WithLabelValues(nodeName, binding).Set(0)
					logger.Info("stopped leading", "vips", addressesToStrings(vips))
				},
				OnNewLeader: func(id string) {
					logger.V(1).Info("new speaker leader", "identity", id)
				},
			},
		})
		if err != nil {
			logger.Error(err, "failed to create leader elector")
			return
		}
		elector.Run(ctx)
	}()

	return r
}

func runResponders(ctx context.Context, iface *net.Interface, vips []netip.Addr, logger logr.Logger) {
	logger.Info("advertising VIPs", "vips", addressesToStrings(vips), "interface", iface.Name)

	var wg sync.WaitGroup
	if hasIPv4(vips) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			runARP(ctx, iface, vips, logger)
		}()
	}
	if hasIPv6(vips) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			runNDP(ctx, iface, vips, logger)
		}()
	}
	wg.Wait()
}

func runARP(ctx context.Context, iface *net.Interface, vips []netip.Addr, logger logr.Logger) {
	for {
		responder, err := newARPResponder(iface, vips)
		if err == nil {
			responder.Run(ctx, logger)
		} else {
			logger.Error(err, "failed to start ARP responder", "interface", iface.Name)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
}

func runNDP(ctx context.Context, iface *net.Interface, vips []netip.Addr, logger logr.Logger) {
	for {
		responder, err := newNDPResponder(iface, vips)
		if err == nil {
			responder.Run(ctx, logger)
		} else {
			logger.Error(err, "failed to start NDP responder", "interface", iface.Name)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
}

func hasIPv4(addresses []netip.Addr) bool {
	for _, addr := range addresses {
		if addr.Is4() {
			return true
		}
	}
	return false
}

func hasIPv6(addresses []netip.Addr) bool {
	for _, addr := range addresses {
		if !addr.Is4() {
			return true
		}
	}
	return false
}

func leaseName(key types.NamespacedName) string {
	sum := sha256.Sum256([]byte(key.Namespace + "/" + key.Name))
	return "easylb-vip-" + hex.EncodeToString(sum[:6])
}

func addressesToStrings(addresses []netip.Addr) []string {
	out := make([]string, 0, len(addresses))
	for _, addr := range addresses {
		out = append(out, addr.String())
	}
	return out
}
