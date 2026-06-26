// FILE: pkg/controller/http01proxy/serviceaccounts.go
//
// WHAT IT DOES (max 5 lines):
// Manages the ServiceAccount for HTTP01 proxy pods. Loads ServiceAccount template from embedded
// YAML, applies labels, and creates/updates it in Kubernetes. ServiceAccount provides identity
// for proxy pods - gets bound to privileged SCC via RoleBinding (created by rbacs.go) which
// grants permissions for hostNetwork and NET_ADMIN capability. Also handles deletion during
// HTTP01Proxy cleanup (finalizer path).
//
// HOW IT DOES IT (max 5 lines):
// createOrApplyServiceAccount decodes ServiceAccount YAML from bindata/http01-proxy/cert-manager-http01-proxy-serviceaccount.yaml,
// sets namespace to match HTTP01Proxy, applies standard labels, then calls createOrUpdateResource
// (server-side apply). deleteServiceAccount removes ServiceAccount by name during cleanup.
// ServiceAccount must exist before DaemonSet is created (pod spec references it), so this runs
// early in deployment sequence (before RBAC and DaemonSet in install_http01proxy.go).

package http01proxy

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

func (r *Reconciler) createOrApplyServiceAccount(ctx context.Context, proxy *v1alpha1.HTTP01Proxy, resourceLabels map[string]string) error {
	sa := common.DecodeObjBytes[*corev1.ServiceAccount](codecs, corev1.SchemeGroupVersion, assets.MustAsset(serviceAccountAssetName))
	sa.SetNamespace(proxy.GetNamespace())
	common.UpdateResourceLabels(sa, resourceLabels)
	return r.createOrUpdateResource(ctx, sa)
}

func (r *Reconciler) deleteServiceAccount(ctx context.Context, proxy *v1alpha1.HTTP01Proxy) error {
	return r.deleteIfExists(ctx, &corev1.ServiceAccount{}, client.ObjectKey{
		Namespace: proxy.GetNamespace(),
		Name:      http01proxyCommonName,
	})
}
