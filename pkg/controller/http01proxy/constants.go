// FILE: pkg/controller/http01proxy/constants.go
//
// WHAT IT DOES (max 5 lines):
// Defines all constants and configuration values used by the HTTP01Proxy controller.
// Centralizes naming conventions (resource names, labels), file paths to embedded
// YAML templates, environment variable names, and timing defaults. Makes the codebase
// maintainable by avoiding magic strings/numbers scattered throughout controller logic.
// Constants defined here are referenced by all other controller files.
//
// HOW IT DOES IT (max 5 lines):
// Declares const and var blocks with named values. Uses const for compile-time
// constants (strings, numbers that never change) and var for values computed at
// runtime (labels map with os.Getenv()). Groups related constants together
// (infrastructure GVK, resource names, asset paths) for readability. Asset names
// match actual files in bindata/ directory (embedded at compile time).

package http01proxy

import (
	"os"   // For reading environment variables (proxy image version)
	"time" // For duration constants (requeue timing)

	"k8s.io/apimachinery/pkg/runtime/schema" // For GroupVersionKind type

	"github.com/openshift/cert-manager-operator/pkg/controller/common" // Shared constants
)

var (
	// infrastructureGVK is the GVK for the OpenShift Infrastructure resource.
	// Used by controller to fetch Infrastructure/cluster object for platform detection
	// Infrastructure.config.openshift.io/v1 contains platform type and VIP addresses
	infrastructureGVK = schema.GroupVersionKind{
		Group:   "config.openshift.io", // OpenShift config API group (not Kubernetes core)
		Version: "v1",                   // Stable version (v1, not v1alpha1)
		Kind:    "Infrastructure",       // Resource kind - cluster-scoped singleton named "cluster"
	}
)

const (
	http01proxyCommonName = "cert-manager-http01-proxy" // Base name used across all resources
	ControllerName        = http01proxyCommonName + "-controller" // Full controller name: "cert-manager-http01-proxy-controller"

	// controllerProcessedAnnotation is added to HTTP01Proxy after successful reconciliation
	// Allows tracking which version of controller last processed the resource
	controllerProcessedAnnotation = "operator.openshift.io/http01-proxy-processed"

	// finalizer prevents deletion until controller completes cleanup (removes DaemonSet, etc.)
	// Format: "<domain>/<controller-name>" per Kubernetes finalizer naming convention
	finalizer                     = "http01proxy.openshift.operator.io/" + ControllerName

	// defaultRequeueTime is how long to wait before re-reconciling on transient errors
	// 30 seconds gives platform time to stabilize without hammering API server
	defaultRequeueTime            = time.Second * 30

	// CRD enforces singleton name "default".
	// Controller only watches for HTTP01Proxy named "default" - others are rejected by CRD validation
	http01proxyObjectName = "default"

	// http01proxyImageNameEnvVarName is the env var containing proxy container image
	// Operator deployment must set this (e.g., quay.io/openshift/cert-manager-http01-proxy:latest)
	// Controller reads this to know which image to deploy in DaemonSet
	http01proxyImageNameEnvVarName    = "RELATED_IMAGE_CERT_MANAGER_HTTP01PROXY"

	// http01proxyImageVersionEnvVarName is the env var containing proxy image version/tag
	// Used for resource labels (helps with tracking deployed version in kubectl get output)
	http01proxyImageVersionEnvVarName = "HTTP01PROXY_OPERAND_IMAGE_VERSION"

	// defaultInternalPort is the port proxy listens on inside the pod (when mode=DefaultDeployment)
	// Unprivileged port (>1024) so proxy doesn't need to run as root just for port binding
	// External traffic hits VIP:80, nftables redirects to this port
	defaultInternalPort int32 = 8888

	// proxyPortName is the name of the port in DaemonSet pod spec (for readability in kubectl output)
	proxyPortName             = "proxy"

	// proxyPortEnvVar is the env var name injected into proxy container with the port number
	// Proxy container reads this to know what port to listen on
	proxyPortEnvVar           = "PROXY_PORT"
)

var (
	// controllerDefaultResourceLabels are applied to all resources created by controller
	// Standard Kubernetes recommended labels (app.kubernetes.io/*) for consistency
	// Makes it easy to query all HTTP01Proxy resources: kubectl get all -l app.kubernetes.io/name=cert-manager-http01-proxy
	controllerDefaultResourceLabels = map[string]string{
		common.ManagedResourceLabelKey: http01proxyCommonName,             // cert-manager-operator's own label key
		"app.kubernetes.io/name":       http01proxyCommonName,             // Primary application name
		"app.kubernetes.io/instance":   http01proxyCommonName,             // Instance name (singleton, so same as name)
		"app.kubernetes.io/version":    os.Getenv(http01proxyImageVersionEnvVarName), // Runtime value: image version from env var
		"app.kubernetes.io/managed-by": "cert-manager-operator",           // Which operator manages this resource
		"app.kubernetes.io/part-of":    "cert-manager-operator",           // Which application this is part of
	}
)

// asset names are the files present in the root bindata/ dir.
// These paths are relative to bindata/ directory, which gets embedded into binary at compile time
// Controller uses these strings to load YAML templates from embedded filesystem
const (
	daemonsetAssetName          = "http01-proxy/cert-manager-http01-proxy-daemonset.yaml"          // DaemonSet template
	serviceAccountAssetName     = "http01-proxy/cert-manager-http01-proxy-serviceaccount.yaml"     // ServiceAccount template
	clusterRoleAssetName        = "http01-proxy/cert-manager-http01-proxy-clusterrole.yaml"        // ClusterRole template
	clusterRoleBindingAssetName = "http01-proxy/cert-manager-http01-proxy-clusterrolebinding.yaml" // ClusterRoleBinding template
	sccRoleBindingAssetName     = "http01-proxy/cert-manager-http01-proxy-scc-rolebinding.yaml"    // SCC RoleBinding template
)

// http01ProxyNetworkPolicyAssets is an array of NetworkPolicy template file paths
// Array allows controller to loop and apply multiple NetworkPolicy manifests
// Two policies: deny-all ingress (default deny), allow-egress on specific ports (least privilege)
var http01ProxyNetworkPolicyAssets = []string{
	"networkpolicies/http01-proxy-deny-all-networkpolicy.yaml",     // Blocks all incoming traffic to proxy pods
	"networkpolicies/http01-proxy-allow-egress-networkpolicy.yaml", // Allows outbound to ports 80,443,6443
}
