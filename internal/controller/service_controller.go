// Package controller reconciles Services and VIPBindings.
package controller

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	easylv1alpha1 "github.com/bzdvdn/easylb/api/v1alpha1"
	"github.com/bzdvdn/easylb/internal/address"
	"github.com/bzdvdn/easylb/internal/filter"
	"github.com/bzdvdn/easylb/internal/vip"
)

// ServiceReconciler publishes node IPs and external VIPs into the
// status.loadBalancer.ingress field of selected LoadBalancer Services.
type ServiceReconciler struct {
	client.Client

	Recorder       record.EventRecorder
	LBClass        string
	VIPSource      vip.Source
	RequireClass   bool
	AddressOptions address.Options
	RequeueAfter   time.Duration
}

// Reconcile implements reconcile.Reconciler.
func (r *ServiceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var svc corev1.Service
	if err := r.Get(ctx, req.NamespacedName, &svc); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !filter.Manages(&svc, filter.Options{LBClass: r.LBClass, RequireClass: r.RequireClass}) {
		return ctrl.Result{}, nil
	}

	var nodeList corev1.NodeList
	if err := r.List(ctx, &nodeList); err != nil {
		return ctrl.Result{}, err
	}

	var sliceList discoveryv1.EndpointSliceList
	if err := r.List(ctx, &sliceList,
		client.InNamespace(svc.Namespace),
		client.MatchingLabels{discoveryv1.LabelServiceName: svc.Name},
	); err != nil {
		return ctrl.Result{}, err
	}

	nodes := make([]*corev1.Node, 0, len(nodeList.Items))
	for i := range nodeList.Items {
		nodes = append(nodes, &nodeList.Items[i])
	}
	slices := make([]*discoveryv1.EndpointSlice, 0, len(sliceList.Items))
	for i := range sliceList.Items {
		slices = append(slices, &sliceList.Items[i])
	}

	var vipSpecs []vip.Spec
	if r.VIPSource != nil {
		var err error
		vipSpecs, err = r.VIPSource.VIPs(ctx, &svc)
		if err != nil {
			return ctrl.Result{}, err
		}
	}
	vips := make([]string, 0, len(vipSpecs))
	for _, spec := range vipSpecs {
		vips = append(vips, spec.IP)
	}
	ips, invalidVIPs := address.DesiredWithVIPs(&svc, nodes, slices, vips, r.AddressOptions)
	if len(invalidVIPs) > 0 && r.Recorder != nil {
		r.Recorder.Eventf(&svc, corev1.EventTypeWarning, "InvalidVIP",
			"ignoring invalid VIP values: %v", invalidVIPs)
	}

	desired := make([]corev1.LoadBalancerIngress, 0, len(ips))
	for _, ip := range ips {
		desired = append(desired, corev1.LoadBalancerIngress{IP: ip})
	}
	if equality.Semantic.DeepEqual(svc.Status.LoadBalancer.Ingress, desired) {
		logger.V(1).Info("status already up to date", "ingress", ips)
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
	}

	original := svc.DeepCopy()
	svc.Status.LoadBalancer.Ingress = desired
	// Optimistic lock prevents a stale leader (or a brief split-brain during a
	// leader election) from overwriting a newer status written by another pod.
	if err := r.Status().Patch(ctx, &svc, client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{})); err != nil {
		return ctrl.Result{}, err
	}
	if r.Recorder != nil {
		r.Recorder.Eventf(&svc, corev1.EventTypeNormal, "AddressesPublished",
			"published load balancer addresses: %v", ips)
	}
	logger.Info("published load balancer addresses", "ingress", ips)
	return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
}

// SetupWithManager registers the controller with the manager.
func (r *ServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Service{}).
		Watches(&corev1.Node{},
			handler.EnqueueRequestsFromMapFunc(r.servicesForNode),
			builder.WithPredicates(nodeChangedPredicate()),
		).
		Watches(&discoveryv1.EndpointSlice{},
			handler.EnqueueRequestsFromMapFunc(r.serviceForEndpointSlice),
		).
		Watches(&easylv1alpha1.VIPBinding{},
			handler.EnqueueRequestsFromMapFunc(r.serviceForBinding),
		).
		Complete(r)
}

func (r *ServiceReconciler) serviceForBinding(_ context.Context, obj client.Object) []reconcile.Request {
	binding, ok := obj.(*easylv1alpha1.VIPBinding)
	if !ok || binding.Spec.ServiceRef.Name == "" {
		return nil
	}
	return []reconcile.Request{{
		NamespacedName: types.NamespacedName{
			Namespace: binding.Namespace,
			Name:      binding.Spec.ServiceRef.Name,
		},
	}}
}

func (r *ServiceReconciler) servicesForNode(ctx context.Context, _ client.Object) []reconcile.Request {
	var services corev1.ServiceList
	if err := r.List(ctx, &services); err != nil {
		return nil
	}
	requests := make([]reconcile.Request, 0, len(services.Items))
	for i := range services.Items {
		if filter.Manages(&services.Items[i], filter.Options{LBClass: r.LBClass, RequireClass: r.RequireClass}) {
			requests = append(requests, reconcile.Request{
				NamespacedName: client.ObjectKeyFromObject(&services.Items[i]),
			})
		}
	}
	return requests
}

func (r *ServiceReconciler) serviceForEndpointSlice(_ context.Context, obj client.Object) []reconcile.Request {
	slice, ok := obj.(*discoveryv1.EndpointSlice)
	if !ok {
		return nil
	}
	name := slice.Labels[discoveryv1.LabelServiceName]
	if name == "" {
		return nil
	}
	return []reconcile.Request{{
		NamespacedName: types.NamespacedName{Namespace: slice.Namespace, Name: name},
	}}
}

func nodeChangedPredicate() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc:  func(event.CreateEvent) bool { return true },
		DeleteFunc:  func(event.DeleteEvent) bool { return true },
		GenericFunc: func(event.GenericEvent) bool { return false },
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldNode, ok := e.ObjectOld.(*corev1.Node)
			if !ok {
				return true
			}
			newNode, ok := e.ObjectNew.(*corev1.Node)
			if !ok {
				return true
			}
			return !equality.Semantic.DeepEqual(oldNode.Status.Addresses, newNode.Status.Addresses) ||
				nodeReady(oldNode) != nodeReady(newNode)
		},
	}
}

func nodeReady(node *corev1.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}
