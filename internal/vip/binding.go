package vip

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	easylv1alpha1 "github.com/bzdvdn/easylb/api/v1alpha1"
)

// BindingSource resolves VIPs from VIPBinding objects in the Service namespace.
type BindingSource struct {
	Client client.Reader
}

// VIPs implements Source.
func (s BindingSource) VIPs(ctx context.Context, svc *corev1.Service) ([]Spec, error) {
	if s.Client == nil || svc == nil {
		return nil, nil
	}

	var list easylv1alpha1.VIPBindingList
	if err := s.Client.List(ctx, &list, client.InNamespace(svc.Namespace)); err != nil {
		return nil, err
	}

	var specs []Spec
	for i := range list.Items {
		binding := &list.Items[i]
		if binding.Spec.ServiceRef.Name != svc.Name {
			continue
		}
		// Publish only addresses validated by the VIPBinding controller.
		// Raw spec.vips may be invalid (for example vips+poolRef).
		ips := binding.Status.AllocatedIPs
		mode := Mode(binding.Spec.Mode)
		if mode == "" {
			mode = ModeExternal
		}
		for _, ip := range ips {
			specs = append(specs, Spec{IP: ip, Mode: mode})
		}
	}
	return dedupeSpecs(specs)
}
