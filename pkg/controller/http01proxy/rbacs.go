// FILE: pkg/controller/http01proxy/rbacs.go
//
// WHAT IT DOES (max 5 lines):
// Manages RBAC resources for HTTP01 proxy: ClusterRole, ClusterRoleBinding, and SCC RoleBinding.
// ClusterRole is empty (proxy doesn't need Kubernetes API access) but follows standard pattern.
// ClusterRoleBinding binds ServiceAccount to ClusterRole. Most importantly, SCC RoleBinding binds
// ServiceAccount to OpenShift's privileged SCC, granting permission to use hostNetwork and NET_ADMIN
// capability (required for nftables manipulation and VIP traffic interception).
//
// HOW IT DOES IT (max 5 lines):
// createOrApplyRBACResources loads three YAML templates from bindata/, applies labels, and creates/
// updates them: ClusterRole (empty rules), ClusterRoleBinding (binds SA to ClusterRole), and
// RoleBinding (binds SA to system:openshift:scc:privileged). The SCC binding is THE critical one -
// without it, OpenShift blocks pods from using hostNetwork. deleteRBACResources removes all three
// during cleanup. Must run after ServiceAccount creation, before DaemonSet.

package http01proxy

import (
	"context"
	"fmt"

	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

func (r *Reconciler) createOrApplyRBACResources(ctx context.Context, proxy *v1alpha1.HTTP01Proxy, resourceLabels map[string]string) error {
	cr := common.DecodeObjBytes[*rbacv1.ClusterRole](codecs, rbacv1.SchemeGroupVersion, assets.MustAsset(clusterRoleAssetName))
	common.UpdateResourceLabels(cr, resourceLabels)
	if err := r.createOrUpdateResource(ctx, cr); err != nil {
		return fmt.Errorf("failed to reconcile clusterrole: %w", err)
	}

	crb := common.DecodeObjBytes[*rbacv1.ClusterRoleBinding](codecs, rbacv1.SchemeGroupVersion, assets.MustAsset(clusterRoleBindingAssetName))
	common.UpdateResourceLabels(crb, resourceLabels)
	for i := range crb.Subjects {
		if crb.Subjects[i].Kind == "ServiceAccount" {
			crb.Subjects[i].Namespace = proxy.GetNamespace()
		}
	}
	if err := r.createOrUpdateResource(ctx, crb); err != nil {
		return fmt.Errorf("failed to reconcile clusterrolebinding: %w", err)
	}

	sccCRB := common.DecodeObjBytes[*rbacv1.ClusterRoleBinding](codecs, rbacv1.SchemeGroupVersion, assets.MustAsset(sccRoleBindingAssetName))
	common.UpdateResourceLabels(sccCRB, resourceLabels)
	for i := range sccCRB.Subjects {
		if sccCRB.Subjects[i].Kind == "ServiceAccount" {
			sccCRB.Subjects[i].Namespace = proxy.GetNamespace()
		}
	}
	if err := r.createOrUpdateResource(ctx, sccCRB); err != nil {
		return fmt.Errorf("failed to reconcile scc clusterrolebinding: %w", err)
	}

	return nil
}

func (r *Reconciler) deleteRBACResources(ctx context.Context) error {
	for _, name := range []string{http01proxyCommonName, http01proxyCommonName + "-scc"} {
		if err := r.deleteIfExists(ctx, &rbacv1.ClusterRoleBinding{}, client.ObjectKey{Name: name}); err != nil {
			return fmt.Errorf("failed to delete clusterrolebinding %q: %w", name, err)
		}
	}
	return r.deleteIfExists(ctx, &rbacv1.ClusterRole{}, client.ObjectKey{Name: http01proxyCommonName})
}
