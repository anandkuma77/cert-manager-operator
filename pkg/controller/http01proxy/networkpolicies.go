// FILE: pkg/controller/http01proxy/networkpolicies.go
//
// WHAT IT DOES (max 5 lines):
// Manages NetworkPolicies for HTTP01 proxy pods implementing defense-in-depth security. Deploys
// two policies: deny-all ingress (blocks all incoming connections - proxy doesn't need any) and
// allow-egress on ports 80,443,6443 (proxy needs to forward to Ingress VIP on port 80, and may
// need HTTPS/K8s API access). Reduces attack surface even though proxy runs privileged - if proxy
// container is compromised, network policies limit lateral movement.
//
// HOW IT DOES IT (max 5 lines):
// createOrApplyNetworkPolicies loops through http01ProxyNetworkPolicyAssets array (two YAML files
// in bindata/networkpolicies/), decodes each template, applies labels, and creates/updates them.
// Deny-all policy matches proxy pods by label selector and blocks all ingress. Allow-egress policy
// permits outbound on specific ports only (least privilege). deleteNetworkPolicies removes both
// during cleanup. Runs first in deployment sequence (no dependencies, establishes security baseline).

package http01proxy

import (
	"context"
	"fmt"

	networkingv1 "k8s.io/api/networking/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

func (r *Reconciler) createOrApplyNetworkPolicies(ctx context.Context, proxy *v1alpha1.HTTP01Proxy, resourceLabels map[string]string) error {
	for _, assetName := range http01ProxyNetworkPolicyAssets {
		np := common.DecodeObjBytes[*networkingv1.NetworkPolicy](codecs, networkingv1.SchemeGroupVersion, assets.MustAsset(assetName))
		np.SetNamespace(proxy.GetNamespace())
		common.UpdateResourceLabels(np, resourceLabels)

		if err := r.createOrUpdateResource(ctx, np); err != nil {
			return fmt.Errorf("failed to reconcile network policy %s: %w", assetName, err)
		}
	}
	return nil
}

func (r *Reconciler) deleteNetworkPolicies(ctx context.Context, proxy *v1alpha1.HTTP01Proxy) error {
	for _, assetName := range http01ProxyNetworkPolicyAssets {
		np := common.DecodeObjBytes[*networkingv1.NetworkPolicy](codecs, networkingv1.SchemeGroupVersion, assets.MustAsset(assetName))
		key := client.ObjectKey{Namespace: proxy.GetNamespace(), Name: np.GetName()}
		if err := r.deleteIfExists(ctx, &networkingv1.NetworkPolicy{}, key); err != nil {
			return fmt.Errorf("failed to delete network policy %q: %w", key.Name, err)
		}
	}
	return nil
}
