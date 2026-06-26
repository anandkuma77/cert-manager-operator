// FILE: pkg/controller/http01proxy/infrastructure.go
//
// WHAT IT DOES (max 5 lines):
// Platform validation - the "gatekeeper" that determines if HTTP01 proxy should deploy.
// Fetches the cluster's Infrastructure/cluster object to discover platform type (BareMetal/AWS/
// Azure/etc.) and VIP addresses. Validates that platform is BareMetal with DISTINCT API and
// Ingress VIPs (if they're the same, proxy isn't needed). Caches result to avoid repeated
// API calls. Returns human-readable error messages when platform is unsupported.
//
// HOW IT DOES IT (max 5 lines):
// Uses unstructured client to read Infrastructure CR (avoiding typed dependencies on
// config.openshift.io API). Parses nested fields from status.platformStatus.baremetal
// to extract apiServerInternalIPs and ingressIPs. Caches result in Reconciler with
// mutex protection (thread-safe). Validation logic checks platform type, VIP presence,
// and VIP distinctness - failing any check blocks deployment and sets Degraded status.

package http01proxy

import (
	"context"  // For cancellation
	"fmt"      // For error formatting

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured" // For reading Infrastructure without typed client
	"k8s.io/apimachinery/pkg/types"                     // For NamespacedName
)

const (
	// platformBareMetal is the platform type for baremetal clusters.
	// Must match exactly what Infrastructure.status.platformStatus.type reports
	// Other platform types: AWS, Azure, GCP, vSphere, None, etc.
	platformBareMetal = "BareMetal"
)

// platformInfo holds the discovered platform details needed to decide
// whether the HTTP01 proxy should be deployed.
// Cached to avoid fetching Infrastructure on every reconcile loop
type platformInfo struct {
	platformType string   // "BareMetal", "AWS", "Azure", etc.
	apiVIPs      []string // API server VIP addresses (e.g., ["192.168.1.100"])
	ingressVIPs  []string // Ingress VIP addresses (e.g., ["192.168.1.101"])
	// Multiple VIPs possible for HA (IPv4 + IPv6 dual-stack)
}

// getOrDiscoverPlatform returns cached platform info, or fetches it on first call.
// Thread-safe: uses mutex to protect cache from concurrent reconcile loops
func (r *Reconciler) getOrDiscoverPlatform(ctx context.Context) (*platformInfo, error) {
	r.platformMu.Lock()                          // Acquire lock before reading/writing cache
	defer r.platformMu.Unlock()                  // Release lock when function returns

	if r.cachedPlatform != nil {                 // Cache hit: platform already discovered
		return r.cachedPlatform, nil             // Return cached result without API call
	}

	// Cache miss: fetch from API server
	info, err := r.discoverPlatform(ctx)
	if err != nil {
		return nil, err                          // Don't cache errors - retry next reconcile
	}

	r.cachedPlatform = info                      // Cache successful result
	return info, nil
}

// discoverPlatform reads the Infrastructure CR and returns platform details.
// Infrastructure/cluster is a cluster-scoped singleton created by OpenShift installer
// Contains platform type (detected during installation) and VIP addresses
func (r *Reconciler) discoverPlatform(ctx context.Context) (*platformInfo, error) {
	// Use unstructured client because we don't import config.openshift.io typed client
	// Avoids dependency bloat - we only need a few fields
	infra := &unstructured.Unstructured{}
	infra.SetGroupVersionKind(infrastructureGVK) // config.openshift.io/v1, Kind=Infrastructure

	// Fetch Infrastructure/cluster (singleton - name is always "cluster")
	if err := r.Get(ctx, types.NamespacedName{Name: "cluster"}, infra); err != nil {
		return nil, fmt.Errorf("failed to get infrastructure/cluster: %w", err)
	}

	// Parse platform type from status.platformStatus.type
	// NestedString navigates JSON path safely, returning (value, found, error)
	platformType, found, err := unstructured.NestedString(infra.Object, "status", "platformStatus", "type")
	if err != nil {
		return nil, fmt.Errorf("failed to parse infrastructure status.platformStatus.type: %w", err)
	}
	if !found {
		return nil, fmt.Errorf("infrastructure status.platformStatus.type not found")
	}

	info := &platformInfo{
		platformType: platformType,              // e.g., "BareMetal", "AWS", "Azure"
	}

	// Only parse VIPs for BareMetal platform (other platforms don't have this structure)
	switch platformType {
	case platformBareMetal:
		// Parse API VIPs from status.platformStatus.baremetal.apiServerInternalIPs
		// These are the VIPs that API server traffic hits (kube-apiserver, oauth-apiserver)
		apiVIPs, _, err := unstructured.NestedStringSlice(infra.Object, "status", "platformStatus", "baremetal", "apiServerInternalIPs")
		if err != nil {
			return nil, fmt.Errorf("failed to parse baremetal.apiServerInternalIPs: %w", err)
		}

		// Parse Ingress VIPs from status.platformStatus.baremetal.ingressIPs
		// These are the VIPs that application traffic hits (*.apps.<cluster-domain>)
		ingressVIPs, _, err := unstructured.NestedStringSlice(infra.Object, "status", "platformStatus", "baremetal", "ingressIPs")
		if err != nil {
			return nil, fmt.Errorf("failed to parse baremetal.ingressIPs: %w", err)
		}

		info.apiVIPs = apiVIPs                   // e.g., ["192.168.1.100", "fd00::100"] (IPv4 + IPv6)
		info.ingressVIPs = ingressVIPs           // e.g., ["192.168.1.101", "fd00::101"]
	}
	// For non-BareMetal platforms, apiVIPs and ingressVIPs remain nil

	return info, nil
}

// validatePlatform checks whether the platform supports HTTP01 proxy deployment.
// Returns a human-readable reason if the platform is not supported, or empty string if OK.
// This is the GATEKEEPER - if validation fails, controller sets Degraded status and stops
func validatePlatform(info *platformInfo) string {
	// Check 1: Must be BareMetal platform
	// AWS/Azure/GCP don't have the API VIP vs Ingress VIP split - traffic already routes correctly
	if info.platformType != platformBareMetal {
		return fmt.Sprintf("platform type %q is not supported; HTTP01 proxy is only supported on BareMetal platforms", info.platformType)
	}

	// Check 2: API VIPs must be present
	// If missing, platform is misconfigured or installation incomplete
	if len(info.apiVIPs) == 0 {
		return "no API server VIPs found in infrastructure status; cannot deploy HTTP01 proxy"
	}

	// Check 3: Ingress VIPs must be present
	// If missing, platform is misconfigured or installation incomplete
	if len(info.ingressVIPs) == 0 {
		return "no ingress VIPs found in infrastructure status; cannot deploy HTTP01 proxy"
	}

	// Check 4: API VIP and Ingress VIP must be DIFFERENT
	// If they're the same, ACME challenges already route correctly - proxy not needed
	// This happens on some bare-metal configurations where admin configured same VIP for both
	for _, apiVIP := range info.apiVIPs {
		for _, ingressVIP := range info.ingressVIPs {
			if apiVIP == ingressVIP {
				// VIPs match - proxy deployment would be pointless (requests already reach ingress)
				return fmt.Sprintf("API VIP (%s) and ingress VIP (%s) are the same; HTTP01 proxy is not needed", apiVIP, ingressVIP)
			}
		}
	}

	// All checks passed - platform is valid for HTTP01 proxy deployment
	return "" // Empty string = validation success
}
