// FILE: pkg/controller/http01proxy/daemonsets.go
//
// WHAT IT DOES (max 5 lines):
// Manages the DaemonSet that runs proxy pods on master nodes. Loads DaemonSet template
// from embedded YAML (bindata), customizes it with proxy image, port configuration, and
// labels, then creates/updates in Kubernetes. This is where the operator configures HOW
// proxy pods run (image, ports, resources). The actual VIP addresses would be injected
// here as environment variables (API_VIP, INGRESS_VIP) - see install_http01proxy.go.
//
// HOW IT DOES IT (max 5 lines):
// Decodes DaemonSet YAML template using Kubernetes codecs, sets namespace and labels,
// injects proxy image from environment variable, updates container port based on mode
// (DefaultDeployment=8888 or CustomDeployment=user-specified). Uses createOrUpdateResource
// to apply DaemonSet to cluster (create if missing, update if exists). Deletion helper
// removes DaemonSet when HTTP01Proxy is deleted (finalizer cleanup path).

package http01proxy

import (
	"context"  // For cancellation
	"fmt"      // For error formatting
	"maps"     // For maps.Copy (Go 1.21+)
	"strconv"  // For converting port int to string for env var

	appsv1 "k8s.io/api/apps/v1"                // DaemonSet types
	corev1 "k8s.io/api/core/v1"                // Container, EnvVar types
	"sigs.k8s.io/controller-runtime/pkg/client" // For client.ObjectKey

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"  // HTTP01Proxy API
	"github.com/openshift/cert-manager-operator/pkg/controller/common"  // Shared utilities
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"    // Embedded YAML assets
)

// createOrApplyDaemonSet creates or updates the DaemonSet for HTTP01 proxy pods
// Called by reconcileHTTP01ProxyDeployment after platform validation passes
func (r *Reconciler) createOrApplyDaemonSet(ctx context.Context, proxy *v1alpha1.HTTP01Proxy, resourceLabels map[string]string) error {
	// Build desired DaemonSet object from template + customizations
	desired, err := r.getDaemonSetObject(proxy, resourceLabels)
	if err != nil {
		return common.NewIrrecoverableError(err, "failed to build daemonset object")
		// NewIrrecoverableError = permanent failure, don't retry (missing env var, etc.)
	}

	// Apply to cluster: create if missing, update if exists (server-side apply semantics)
	if err := r.createOrUpdateResource(ctx, desired); err != nil {
		return err                               // Transient errors (API server down) will retry
	}

	// Update HTTP01Proxy status with deployed image version (informational for users)
	r.updateImageInStatus(proxy, desired)
	return nil
}

// getDaemonSetObject builds the DaemonSet from template and applies customizations
// Returns fully-configured DaemonSet ready to apply to cluster
func (r *Reconciler) getDaemonSetObject(proxy *v1alpha1.HTTP01Proxy, resourceLabels map[string]string) (*appsv1.DaemonSet, error) {
	// Load DaemonSet YAML template from embedded asset (bindata/http01-proxy/cert-manager-http01-proxy-daemonset.yaml)
	// DecodeObjBytes deserializes YAML → Go struct using Kubernetes codecs
	// MustAsset panics if asset missing (compile-time embedded, so safe)
	ds := common.DecodeObjBytes[*appsv1.DaemonSet](codecs, appsv1.SchemeGroupVersion, assets.MustAsset(daemonsetAssetName))

	// Set namespace to match HTTP01Proxy (inherits from parent)
	ds.SetNamespace(proxy.GetNamespace())

	// Apply standard labels (app.kubernetes.io/name, managed-by, etc.) to DaemonSet metadata
	common.UpdateResourceLabels(ds, resourceLabels)

	// Also apply labels to pod template (for kubectl get pods -l app.kubernetes.io/name=...)
	if ds.Spec.Template.Labels == nil {
		ds.Spec.Template.Labels = make(map[string]string) // Initialize if template has no labels
	}
	maps.Copy(ds.Spec.Template.Labels, resourceLabels) // Merge labels into pod template

	// Inject proxy container image from environment variable
	// Operator deployment must set RELATED_IMAGE_CERT_MANAGER_HTTP01PROXY env var
	if r.proxyImage == "" {
		// Missing env var = operator misconfiguration, fail permanently
		return nil, fmt.Errorf("environment variable %s is not set", http01proxyImageNameEnvVarName)
	}
	if len(ds.Spec.Template.Spec.Containers) == 0 {
		// Template asset has no containers = broken asset file, fail permanently
		return nil, fmt.Errorf("DaemonSet asset %s has no containers defined", daemonsetAssetName)
	}
	// Set container image on first container (proxy is single-container pod)
	ds.Spec.Template.Spec.Containers[0].Image = r.proxyImage // e.g., "quay.io/openshift/cert-manager-http01-proxy:v1.0"

	// Get internal port from HTTP01Proxy spec (DefaultDeployment=8888, CustomDeployment=user-specified)
	port := r.getInternalPort(proxy)

	// Update DaemonSet container port and env var with chosen port
	r.updateDaemonSetPort(ds, port)

	// NOTE: VIP injection (API_VIP, INGRESS_VIP env vars) happens in install_http01proxy.go
	// after this function returns - this file only handles image/port configuration

	return ds, nil
}

// updateDaemonSetPort updates container port and PROXY_PORT env var with specified port
// Called for both DefaultDeployment (port=8888) and CustomDeployment (port=user-specified)
func (r *Reconciler) updateDaemonSetPort(ds *appsv1.DaemonSet, port int32) {
	container := &ds.Spec.Template.Spec.Containers[0] // First (only) container in pod

	// Update containerPort and hostPort in ports list
	// hostPort: exposes port on host network (required for VIP traffic interception)
	// containerPort: port inside container (proxy listens here)
	for i := range container.Ports {
		if container.Ports[i].Name == proxyPortName { // Find port named "proxy"
			container.Ports[i].ContainerPort = port    // Port inside container
			container.Ports[i].HostPort = port         // Port on host (must match for hostNetwork)
			// With hostNetwork: true, containerPort and hostPort must match
		}
	}

	// Update PROXY_PORT environment variable (proxy container reads this to know what port to bind)
	portStr := strconv.FormatInt(int64(port), 10) // Convert int32 → string (env vars are strings)
	envUpdated := false

	// Search for existing PROXY_PORT env var and update it
	for i := range container.Env {
		if container.Env[i].Name == proxyPortEnvVar { // proxyPortEnvVar = "PROXY_PORT"
			container.Env[i].Value = portStr           // Update existing env var
			envUpdated = true
			break
		}
	}

	// If PROXY_PORT not found in template, append it (defensive - template should have it)
	if !envUpdated {
		container.Env = append(container.Env, corev1.EnvVar{
			Name:  proxyPortEnvVar,  // "PROXY_PORT"
			Value: portStr,           // e.g., "8888" or "9000"
		})
	}
}

// updateImageInStatus writes deployed proxy image to HTTP01Proxy.status.proxyImage
// Informational field - lets users see which image version is running via kubectl get
func (r *Reconciler) updateImageInStatus(proxy *v1alpha1.HTTP01Proxy, ds *appsv1.DaemonSet) {
	if len(ds.Spec.Template.Spec.Containers) > 0 {
		// Extract image from first container and write to status
		proxy.Status.ProxyImage = ds.Spec.Template.Spec.Containers[0].Image
		// e.g., "quay.io/openshift/cert-manager-http01-proxy:v1.0.0"
		// This gets persisted to API server by updateCondition() in controller.go
	}
}

// deleteDaemonSet removes the HTTP01 proxy DaemonSet during cleanup
// Called by cleanUp() when HTTP01Proxy is deleted (finalizer cleanup path)
func (r *Reconciler) deleteDaemonSet(ctx context.Context, proxy *v1alpha1.HTTP01Proxy) error {
	// deleteIfExists is a helper that deletes resource if present, succeeds if already gone
	return r.deleteIfExists(ctx, &appsv1.DaemonSet{}, client.ObjectKey{
		Namespace: proxy.GetNamespace(),           // cert-manager-operator
		Name:      http01proxyCommonName,         // cert-manager-http01-proxy
	})
	// Idempotent: safe to call multiple times, no error if DaemonSet already deleted
}
