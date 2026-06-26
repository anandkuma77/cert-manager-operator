// FILE: pkg/controller/http01proxy/install_http01proxy.go
//
// WHAT IT DOES (max 5 lines):
// Orchestrates the deployment of all HTTP01 proxy resources in the correct order.
// Entry point for actual deployment work - called by controller.go after finalizer
// is added. Validates platform first (gatekeeper), then deploys resources sequentially:
// NetworkPolicies → ServiceAccount → RBAC → DaemonSet. If platform validation fails,
// cleans up any existing resources and sets Degraded status.
//
// HOW IT DOES IT (max 5 lines):
// reconcileHTTP01ProxyDeployment is called by processReconcileRequest in controller.go.
// First calls getOrDiscoverPlatform (caches result) and validatePlatform (checks BareMetal
// with distinct VIPs). On validation failure, returns IrrecoverableError (sets Degraded,
// no retry). On success, calls create/apply functions in dependency order. Each create
// function is idempotent (safe to call repeatedly). Adds processed annotation on success.

package http01proxy

import (
	"context"  // For cancellation
	"fmt"      // For error formatting
	"maps"     // For maps.Copy

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"  // HTTP01Proxy API
	"github.com/openshift/cert-manager-operator/pkg/controller/common"  // Error types, utilities
)

// reconcileHTTP01ProxyDeployment is the main reconciliation entry point
// Called by processReconcileRequest in controller.go after finalizer is ensured
// Returns error for retry, or IrrecoverableError for permanent failure (sets Degraded)
func (r *Reconciler) reconcileHTTP01ProxyDeployment(ctx context.Context, proxy *v1alpha1.HTTP01Proxy) error {
	// STEP 1: Discover platform type and VIP addresses
	// getOrDiscoverPlatform returns cached result on subsequent calls (avoids repeated API fetches)
	// Reads Infrastructure/cluster and extracts platformType, apiVIPs, ingressVIPs
	info, err := r.getOrDiscoverPlatform(ctx)
	if err != nil {
		// Transient error (API server down, Infrastructure not found) - retry after defaultRequeueTime
		return common.NewRetryRequiredError(err, "failed to discover platform")
	}

	// STEP 2: Validate platform compatibility (THE GATEKEEPER)
	// validatePlatform checks: BareMetal? VIPs present? VIPs distinct?
	// Returns human-readable reason string if invalid, empty string if valid
	if reason := validatePlatform(info); reason != "" {
		r.log.V(1).Info("platform not supported for HTTP01 proxy", "reason", reason, "platformType", info.platformType)

		// Platform is invalid (e.g., AWS, or BareMetal with same VIPs)
		// Clean up any resources that might have been deployed before platform changed
		if err := r.cleanUp(ctx, proxy); err != nil {
			r.log.Error(err, "failed to clean up resources after platform validation failure")
			// Log error but don't fail reconciliation - cleanup is best-effort
		}

		// Return IrrecoverableError: sets status.conditions Degraded=True with reason message
		// Controller won't retry (no point - platform won't change without cluster reinstall)
		return common.NewIrrecoverableError(fmt.Errorf("platform validation failed"), "%s", reason)
	}

	// Platform is valid - proceed with deployment

	// STEP 3: Build resource labels (applied to all created resources)
	// Labels help with:
	// - kubectl get all -l app.kubernetes.io/name=cert-manager-http01-proxy
	// - Owner references (garbage collection)
	// - Identifying controller-managed resources
	resourceLabels := make(map[string]string)
	maps.Copy(resourceLabels, controllerDefaultResourceLabels) // Copy from constants.go

	// STEP 4: Deploy resources in dependency order
	// NetworkPolicies first (no dependencies, defense-in-depth before pods start)
	if err := r.createOrApplyNetworkPolicies(ctx, proxy, resourceLabels); err != nil {
		r.log.Error(err, "failed to reconcile network policy resources")
		return err  // Retry on failure
	}

	// ServiceAccount second (RBAC will reference it)
	if err := r.createOrApplyServiceAccount(ctx, proxy, resourceLabels); err != nil {
		r.log.Error(err, "failed to reconcile serviceaccount resource")
		return err  // Retry on failure
	}

	// RBAC third (binds ServiceAccount to ClusterRole and privileged SCC)
	// Must happen before DaemonSet or pods will fail to start (permission denied for hostNetwork)
	if err := r.createOrApplyRBACResources(ctx, proxy, resourceLabels); err != nil {
		r.log.Error(err, "failed to reconcile rbac resources")
		return err  // Retry on failure
	}

	// DaemonSet last (depends on ServiceAccount and RBAC being ready)
	// This is where proxy pods get deployed with VIP addresses as environment variables
	// (VIP injection logic would be in getDaemonSetObject or a future enhancement)
	if err := r.createOrApplyDaemonSet(ctx, proxy, resourceLabels); err != nil {
		r.log.Error(err, "failed to reconcile daemonset resource")
		return err  // Retry on failure
	}

	// STEP 5: Mark HTTP01Proxy as processed
	// Add annotation to track that controller successfully reconciled this object
	// AddAnnotation returns true if annotation was added (wasn't present before)
	if common.AddAnnotation(proxy, controllerProcessedAnnotation, "true") {
		// Persist annotation to API server
		if err := r.UpdateWithRetry(ctx, proxy); err != nil {
			return fmt.Errorf("failed to update processed annotation to %s/%s: %w", proxy.GetNamespace(), proxy.GetName(), err)
		}
	}

	r.log.V(4).Info("finished reconciliation of http01proxy", "namespace", proxy.GetNamespace(), "name", proxy.GetName())
	return nil  // Success - status will be set to Available in controller.go
}
