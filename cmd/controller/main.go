// Command controller runs the easyLB controller.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	easylv1alpha1 "github.com/bzdvdn/easylb/api/v1alpha1"
	"github.com/bzdvdn/easylb/internal/address"
	"github.com/bzdvdn/easylb/internal/controller"
	"github.com/bzdvdn/easylb/internal/vip"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(easylv1alpha1.AddToScheme(scheme))
}

func main() {
	var (
		metricsAddr             string
		probeAddr               string
		leaderElect             bool
		leaderElectionNamespace string
		leaseDuration           time.Duration
		renewDeadline           time.Duration
		retryPeriod             time.Duration
		lbClass                 string
		requireClass            bool
		nodeIPTypes             string
		publishNodeIPs          bool
		enableGatewayAPI        bool
		requeueAfter            time.Duration
	)

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metrics endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the health probe endpoint binds to.")
	flag.BoolVar(&leaderElect, "leader-elect", false, "Enable leader election for the controller manager.")
	flag.StringVar(&leaderElectionNamespace, "leader-election-namespace", "easylb-system", "Namespace for the leader election Lease.")
	flag.DurationVar(&leaseDuration, "leader-election-lease-duration", 15*time.Second, "Leader election lease duration.")
	flag.DurationVar(&renewDeadline, "leader-election-renew-deadline", 10*time.Second, "Leader election renew deadline.")
	flag.DurationVar(&retryPeriod, "leader-election-retry-period", 2*time.Second, "Leader election retry period.")
	flag.StringVar(&lbClass, "load-balancer-class", "easylb.io/easylb", "LoadBalancerClass handled by easyLB.")
	flag.BoolVar(&requireClass, "require-class", true, "Only handle Services with the configured loadBalancerClass.")
	flag.StringVar(&nodeIPTypes, "node-ip-types", "InternalIP", "Comma-separated node address types to publish (InternalIP, ExternalIP).")
	flag.BoolVar(&publishNodeIPs, "publish-node-ips", true, "Publish node IPs in addition to VIPs.")
	flag.BoolVar(&enableGatewayAPI, "enable-gateway-api", false, "Enable the optional Gateway API -> VIPBinding integration. Requires the Gateway API v1 CRDs.")
	flag.DurationVar(&requeueAfter, "requeue-after", 5*time.Minute, "Periodic reconcile interval.")
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseDevMode(false)))

	addressTypes, err := parseNodeIPTypes(nodeIPTypes)
	if err != nil {
		ctrl.Log.Error(err, "invalid --node-ip-types")
		os.Exit(1)
	}
	if requeueAfter <= 0 {
		requeueAfter = 5 * time.Minute
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                        scheme,
		Metrics:                       metricsserver.Options{BindAddress: metricsAddr},
		HealthProbeBindAddress:        probeAddr,
		LeaderElection:                leaderElect,
		LeaderElectionID:              "easylb.io",
		LeaderElectionNamespace:       leaderElectionNamespace,
		LeaderElectionReleaseOnCancel: true,
		LeaseDuration:                 ptr.To(leaseDuration),
		RenewDeadline:                 ptr.To(renewDeadline),
		RetryPeriod:                   ptr.To(retryPeriod),
	})
	if err != nil {
		ctrl.Log.Error(err, "unable to create manager")
		os.Exit(1)
	}

	reconciler := &controller.ServiceReconciler{
		Client:         mgr.GetClient(),
		Recorder:       mgr.GetEventRecorderFor("easylb"),
		LBClass:        lbClass,
		VIPSource:      vip.BindingSource{Client: mgr.GetClient()},
		RequireClass:   requireClass,
		AddressOptions: address.Options{NodeIPTypes: addressTypes, PublishNodeIPs: publishNodeIPs},
		RequeueAfter:   requeueAfter,
	}
	if err := reconciler.SetupWithManager(mgr); err != nil {
		ctrl.Log.Error(err, "unable to create Service controller")
		os.Exit(1)
	}

	bindingReconciler := &controller.VIPBindingReconciler{
		Client:       mgr.GetClient(),
		RequeueAfter: time.Minute,
	}
	if err := bindingReconciler.SetupWithManager(mgr); err != nil {
		ctrl.Log.Error(err, "unable to create VIPBinding controller")
		os.Exit(1)
	}

	if enableGatewayAPI {
		gatewayReconciler := &controller.GatewayReconciler{
			Client:       mgr.GetClient(),
			Recorder:     mgr.GetEventRecorderFor("easylb-gateway"),
			RequeueAfter: requeueAfter,
		}
		if err := gatewayReconciler.SetupWithManager(mgr); err != nil {
			ctrl.Log.Error(err, "unable to create Gateway controller")
			os.Exit(1)
		}
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		ctrl.Log.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		ctrl.Log.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	ctrl.Log.Info("starting easyLB",
		"loadBalancerClass", lbClass,
		"nodeIPTypes", nodeIPTypes,
		"publishNodeIPs", publishNodeIPs,
		"gatewayAPI", enableGatewayAPI,
		"leaderElect", leaderElect,
		"leaderElectionNamespace", leaderElectionNamespace,
	)
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		ctrl.Log.Error(err, "manager exited")
		os.Exit(1)
	}
}

func parseNodeIPTypes(value string) ([]corev1.NodeAddressType, error) {
	var types []corev1.NodeAddressType
	for _, part := range strings.Split(value, ",") {
		switch strings.TrimSpace(part) {
		case "":
			continue
		case string(corev1.NodeInternalIP):
			types = append(types, corev1.NodeInternalIP)
		case string(corev1.NodeExternalIP):
			types = append(types, corev1.NodeExternalIP)
		default:
			return nil, fmt.Errorf("unsupported node address type %q (supported: InternalIP, ExternalIP)", part)
		}
	}
	if len(types) == 0 {
		return nil, fmt.Errorf("at least one node address type is required")
	}
	return types, nil
}
