// FILE: api/operator/v1alpha1/features.go
//
// WHAT IT DOES (max 5 lines):
// Defines feature gates for the cert-manager-operator, which are toggleable
// switches that enable/disable optional functionality. HTTP01Proxy is declared
// here as an Alpha feature (disabled by default). Users must explicitly enable
// it via --unsupported-addon-features=HTTP01Proxy=true flag when starting the
// operator to activate the HTTP01Proxy controller.
//
// HOW IT DOES IT (max 5 lines):
// Declares feature gate constants (FeatureHTTP01Proxy, etc.) and registers them
// in a map with their default state and maturity level (Alpha/TechPreview/GA).
// The operator's startup code reads this map to determine which controllers to
// start. Alpha features default to false for safety - prevents accidental activation
// of incomplete/unstable features in production clusters.

package v1alpha1

import (
	"k8s.io/component-base/featuregate" // Kubernetes standard feature gate library
)

var (
	// FeatureIstioCSR enables the controller for istiocsr.operator.openshift.io resource,
	// which extends cert-manager-operator to deploy and manage the istio-csr agent.
	// OpenShift Service Mesh facilitates the integration and istio-csr is an agent that
	// allows Istio workload and control plane components to be secured using cert-manager.
	//
	// For more details,
	// https://github.com/openshift/enhancements/blob/master/enhancements/cert-manager/istio-csr-controller.md
	FeatureIstioCSR featuregate.Feature = "IstioCSR" // String constant: feature gate name

	// FeatureTrustManager enables the controller for trustmanagers.operator.openshift.io resource,
	// which extends cert-manager-operator to deploy and manage the trust-manager operand.
	// trust-manager provides a way to manage trust bundles in OpenShift clusters.
	//
	// For more details,
	// https://github.com/openshift/enhancements/blob/master/enhancements/cert-manager/trust-manager-controller.md
	FeatureTrustManager featuregate.Feature = "TrustManager" // String constant: feature gate name

	// HTTP01Proxy enables the controller for http01proxies.operator.openshift.io resource,
	// which extends cert-manager-operator to deploy and manage the HTTP01 challenge proxy.
	// The proxy enables cert-manager to complete HTTP01 ACME challenges for the API endpoint
	// on baremetal platforms where the API VIP is not exposed via OpenShift Ingress.
	//
	// For more details,
	// https://github.com/openshift/enhancements/pull/1929
	FeatureHTTP01Proxy featuregate.Feature = "HTTP01Proxy" // NEW: String constant for HTTP01Proxy feature
)

// OperatorFeatureGates maps feature gate names to their specifications
// This map is read by operator startup code (pkg/operator/setup_manager.go)
// to determine which controllers to register based on enabled feature gates
var OperatorFeatureGates = map[featuregate.Feature]featuregate.FeatureSpec{
	FeatureIstioCSR: {Default: true, PreRelease: featuregate.GA}, // GA: Generally Available, enabled by default, stable
	FeatureTrustManager: {Default: false, PreRelease: "TechPreview"}, // TechPreview: disabled by default, may change
	FeatureHTTP01Proxy:  {Default: false, PreRelease: featuregate.Alpha}, // NEW: Alpha: disabled by default, unstable API
	// ↑ Default: false means operator won't start HTTP01Proxy controller unless explicitly enabled
	// ↑ PreRelease: featuregate.Alpha means feature is experimental, API may change without notice
	// Users must pass --unsupported-addon-features=HTTP01Proxy=true to enable
}
