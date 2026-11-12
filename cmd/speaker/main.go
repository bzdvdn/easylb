// Command speaker runs the easyLB L2 speaker.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	easylv1alpha1 "github.com/bzdvdn/easylb/api/v1alpha1"
	"github.com/bzdvdn/easylb/internal/speaker"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(easylv1alpha1.AddToScheme(scheme))
}

func main() {
	var (
		metricsAddr    string
		probeAddr      string
		nodeName       string
		podName        string
		interfaceName  string
		nodeIPType     string
		lbClass        string
		requireClass   bool
		leaseNamespace string
		leaseDuration  time.Duration
		renewDeadline  time.Duration
		retryPeriod    time.Duration
		requeueAfter   time.Duration
	)

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8082", "The address the metrics endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8083", "The address the health probe endpoint binds to.")
	flag.StringVar(&nodeName, "node-name", os.Getenv("NODE_NAME"), "Name of the node this speaker runs on.")
	flag.StringVar(&podName, "pod-name", os.Getenv("POD_NAME"), "Name of this speaker pod (used as the lease identity).")
	flag.StringVar(&interfaceName, "interface", "", "Network interface for ARP. Auto-detected from the node IP when empty.")
	flag.StringVar(&nodeIPType, "node-ip-type", string(corev1.NodeInternalIP), "Node address type used to find the ARP interface.")
	flag.StringVar(&lbClass, "load-balancer-class", "easylb.io/easylb", "LoadBalancerClass handled by easyLB.")
	flag.BoolVar(&requireClass, "require-class", true, "Only handle Services with the configured loadBalancerClass.")
	flag.StringVar(&leaseNamespace, "lease-namespace", "easylb-system", "Namespace for per-VIP leader election Leases.")
	flag.DurationVar(&leaseDuration, "lease-duration", 15*time.Second, "VIP leader election lease duration.")
	flag.DurationVar(&renewDeadline, "renew-deadline", 10*time.Second, "VIP leader election renew deadline.")
	flag.DurationVar(&retryPeriod, "retry-period", 2*time.Second, "VIP leader election retry period.")
	flag.DurationVar(&requeueAfter, "requeue-after", 30*time.Second, "Periodic reconcile interval.")
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseDevMode(false)))
	ctx := ctrl.SetupSignalHandler()

	if nodeName == "" {
		ctrl.Log.Error(fmt.Errorf("--node-name is required"), "invalid configuration")
		os.Exit(1)
	}
	addressType, err := parseNodeIPType(nodeIPType)
	if err != nil {
		ctrl.Log.Error(err, "invalid --node-ip-type")
		os.Exit(1)
	}
	if requeueAfter <= 0 {
		requeueAfter = 30 * time.Second
	}

	cfg := ctrl.GetConfigOrDie()
	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		ctrl.Log.Error(err, "unable to create Kubernetes client")
		os.Exit(1)
	}

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsserver.Options{BindAddress: metricsAddr},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         false,
	})
	if err != nil {
		ctrl.Log.Error(err, "unable to create manager")
		os.Exit(1)
	}

	reconciler := &speaker.Reconciler{
		Client:         mgr.GetClient(),
		KubeClient:     kubeClient,
		Recorder:       mgr.GetEventRecorderFor("easylb-speaker"),
		Ctx:            ctx,
		NodeName:       nodeName,
		PodName:        podName,
		LBClass:        lbClass,
		RequireClass:   requireClass,
		InterfaceName:  interfaceName,
		NodeIPType:     addressType,
		LeaseNamespace: leaseNamespace,
		LeaseDuration:  leaseDuration,
		RenewDeadline:  renewDeadline,
		RetryPeriod:    retryPeriod,
		RequeueAfter:   requeueAfter,
	}
	if err := reconciler.SetupWithManager(mgr); err != nil {
		ctrl.Log.Error(err, "unable to create speaker controller")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		ctrl.Log.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		ctrl.Log.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	ctrl.Log.Info("starting easyLB speaker",
		"node", nodeName,
		"pod", podName,
		"interface", interfaceName,
		"nodeIPType", nodeIPType,
		"leaseNamespace", leaseNamespace,
	)
	if err := mgr.Start(ctx); err != nil {
		ctrl.Log.Error(err, "manager exited")
		os.Exit(1)
	}
}

func parseNodeIPType(value string) (corev1.NodeAddressType, error) {
	switch corev1.NodeAddressType(value) {
	case corev1.NodeInternalIP:
		return corev1.NodeInternalIP, nil
	case corev1.NodeExternalIP:
		return corev1.NodeExternalIP, nil
	default:
		return "", fmt.Errorf("unsupported node address type %q (supported: InternalIP, ExternalIP)", value)
	}
}
