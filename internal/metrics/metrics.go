// Package metrics defines the Prometheus metrics exposed by easyLB.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	crmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	// VIPAllocations counts VIPBinding allocation results.
	VIPAllocations = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "easylb",
		Subsystem: "controller",
		Name:      "vip_allocations_total",
		Help:      "Number of VIPBinding allocation attempts by result and reason.",
	}, []string{"result", "reason"})

	// AutoBindings counts automatic VIPBinding lifecycle actions.
	AutoBindings = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "easylb",
		Subsystem: "controller",
		Name:      "auto_bindings_total",
		Help:      "Number of automatic VIPBinding lifecycle actions.",
	}, []string{"action"})

	// SpeakerAdvertisedVIPs reports the number of VIPs advertised per binding.
	SpeakerAdvertisedVIPs = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "easylb",
		Subsystem: "speaker",
		Name:      "advertised_vips",
		Help:      "Number of VIPs currently advertised by the speaker per binding.",
	}, []string{"node", "binding"})

	// SpeakerLeaderChanges counts speaker leadership acquisitions.
	SpeakerLeaderChanges = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "easylb",
		Subsystem: "speaker",
		Name:      "leader_changes_total",
		Help:      "Number of times the speaker acquired leadership for a VIPBinding.",
	}, []string{"node"})

	// SpeakerErrors counts ARP/NDP responder errors.
	SpeakerErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "easylb",
		Subsystem: "speaker",
		Name:      "errors_total",
		Help:      "Number of ARP/NDP responder errors by interface, protocol, and operation.",
	}, []string{"interface", "protocol", "operation"})
)

func init() {
	crmetrics.Registry.MustRegister(
		VIPAllocations,
		AutoBindings,
		SpeakerAdvertisedVIPs,
		SpeakerLeaderChanges,
		SpeakerErrors,
	)
}
