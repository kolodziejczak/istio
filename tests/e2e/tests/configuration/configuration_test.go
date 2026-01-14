package configuration

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/e2e-framework/klient/k8s/resources"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/klient/wait/conditions"

	"github.com/kyma-project/istio/operator/api/v1alpha2"
	"github.com/kyma-project/istio/operator/tests/e2e/pkg/asserts/extauth"
	"github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/client"
	extauthhelper "github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/extauth"
	gatewayhelper "github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/gateway"
	"github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/httpbin"
	infrahelpers "github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/infrastructure"
	"github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/log"
	modulehelpers "github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/modules"
	"github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/namespace"
	"github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/portforward"
	virtualservice "github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/virtual_service"
)

func TestConfiguration(t *testing.T) {
	// Scenario: Updating proxy resource configuration
	t.Run("Updating proxy resource configuration", func(t *testing.T) {
		c, err := client.ResourcesClient(t)
		require.NoError(t, err)
		istioCR, err := modulehelpers.NewIstioCRBuilder().
			WithProxyResources("30m", "190Mi", "700m", "700Mi").
			ApplyAndCleanup(t)
		require.NoError(t, err)

		err = namespace.LabelNamespaceWithIstioInjection(t, "default")
		require.NoError(t, err)

		_, err = httpbin.NewBuilder().DeployWithCleanup(t)
		require.NoError(t, err)

		_, err = httpbin.NewBuilder().WithName("httpbin-regular-sidecar").WithRegularSidecar().DeployWithCleanup(t)
		require.NoError(t, err)

		assertProxyResourcesForDeployment(t, c, "httpbin", "default", "30m", "190Mi", "700m", "700Mi")

		assertProxyResourcesForDeployment(t, c, "httpbin-regular-sidecar", "default", "30m", "190Mi", "700m", "700Mi")

		// When: Update Istio CR with new proxy resource values
		err = modulehelpers.NewIstioCRBuilder().
			WithName(istioCR.Name).
			WithNamespace(istioCR.Namespace).
			WithProxyResources("80m", "230Mi", "900m", "900Mi").
			Update(t)
		require.NoError(t, err)

		// Then: Verify updated proxy resources for both deployments
		assertProxyResourcesForDeployment(t, c, "httpbin", "default", "80m", "230Mi", "900m", "900Mi")
		assertProxyResourcesForDeployment(t, c, "httpbin-regular-sidecar", "default", "80m", "230Mi", "900m", "900Mi")
	})

	// Scenario: Ingress Gateway adds correct X-Envoy-External-Address header after updating numTrustedProxies
	t.Run("Ingress Gateway X-Envoy-External-Address with numTrustedProxies", func(t *testing.T) {
		// Create Istio CR with NumTrustedProxies=1
		istioCR, err := modulehelpers.NewIstioCRBuilder().
			WithNumTrustedProxies(1).
			ApplyAndCleanup(t)
		require.NoError(t, err)
		// Given: Create namespace and deploy httpbin
		err = namespace.LabelNamespaceWithIstioInjection(t, "default")
		require.NoError(t, err)

		_, _, err = httpbin.DeployHttpbin(t, "default")
		require.NoError(t, err)

		c, err := client.ResourcesClient(t)
		require.NoError(t, err)

		// Wait for httpbin deployment to be ready
		httpbinDeployment := &v1.Deployment{}
		err = c.Get(t.Context(), "httpbin", "default", httpbinDeployment)
		require.NoError(t, err)
		err = wait.For(conditions.New(c).DeploymentConditionMatch(httpbinDeployment, v1.DeploymentAvailable, corev1.ConditionTrue), wait.WithContext(t.Context()))
		require.NoError(t, err)

		// Create gateway and virtual service
		err = gatewayhelper.CreateHTTPGateway(t)
		require.NoError(t, err)

		err = virtualservice.CreateVirtualService(
			t,
			"test-vs",
			"default",
			"httpbin.default.svc.cluster.local",
			[]string{"httpbin.default.svc.cluster.local"},
			[]string{"default/test-gateway"},
		)
		require.NoError(t, err)

		gatewayDomain, gatewayPort, err := portforward.CreateIngressGatewayPortForwarding(t)
		require.NoError(t, err)

		// Verify X-Envoy-External-Address with numTrustedProxies=1
		assertEnvoyExternalAddress(t, gatewayDomain, gatewayPort, "10.2.1.1,10.0.0.1", "10.0.0.1")

		// When: Update numTrustedProxies to 2
		err = modulehelpers.NewIstioCRBuilder().
			WithName(istioCR.Name).
			WithNamespace(istioCR.Namespace).
			WithNumTrustedProxies(2).
			Update(t)
		require.NoError(t, err)

		// Then: Verify X-Envoy-External-Address with numTrustedProxies=2
		assertEnvoyExternalAddress(t, gatewayDomain, gatewayPort, "10.2.1.1,10.0.0.1", "10.2.1.1")
	})

	// Scenario: Egress Gateway has correct configuration
	t.Run("Egress Gateway enabled", func(t *testing.T) {
		// When: Create Istio CR with egress gateway enabled
		enabled := true
		_, err := modulehelpers.NewIstioCRBuilder().
			WithEgressGateway(&v1alpha2.EgressGateway{
				Enabled: &enabled,
			}).
			ApplyAndCleanup(t)
		require.NoError(t, err)

		c, err := client.ResourcesClient(t)
		require.NoError(t, err)

		// Then: Verify egress gateway deployment is ready
		egressDeployment := &v1.Deployment{}
		err = c.Get(t.Context(), "istio-egressgateway", "istio-system", egressDeployment)
		require.NoError(t, err)
		err = wait.For(conditions.New(c).DeploymentConditionMatch(egressDeployment, v1.DeploymentAvailable, corev1.ConditionTrue), wait.WithContext(t.Context()))
		require.NoError(t, err)
	})

	// Scenario: External authorizer (first variant with ext-authz provider)
	t.Run("External authorizer with ext-authz provider", func(t *testing.T) {
		// Given: Create namespace and enable istio injection
		err := infrahelpers.CreateNamespace(t, "default", infrahelpers.WithSidecarInjectionEnabled())
		require.NoError(t, err)

		// Deploy httpbin
		_, _, err = httpbin.DeployHttpbin(t, "default")
		require.NoError(t, err)

		c, err := client.ResourcesClient(t)
		require.NoError(t, err)

		// Wait for httpbin deployment to be ready
		httpbinDeployment := &v1.Deployment{}
		err = c.Get(t.Context(), "httpbin", "default", httpbinDeployment)
		require.NoError(t, err)
		err = wait.For(conditions.New(c).DeploymentConditionMatch(httpbinDeployment, v1.DeploymentAvailable, corev1.ConditionTrue), wait.WithContext(t.Context()))
		require.NoError(t, err)

		// Deploy ext-authz application
		err = extauthhelper.CreateExtAuth(t)
		require.NoError(t, err)

		// Create Istio CR with external authorizer
		authorizer := &v1alpha2.Authorizer{
			Name:    "ext-authz",
			Port:    8000,
			Service: "ext-authz.ext-auth.svc.cluster.local",
		}
		_, err = modulehelpers.NewIstioCRBuilder().
			WithAuthorizer(authorizer).
			ApplyAndCleanup(t)
		require.NoError(t, err)

		// Create gateway and virtual service
		err = gatewayhelper.CreateHTTPGateway(t)
		require.NoError(t, err)

		err = virtualservice.CreateVirtualService(
			t,
			"httpbin",
			"default",
			"httpbin.default.svc.cluster.local",
			[]string{"httpbin.default.svc.cluster.local"},
			[]string{"default/test-gateway"},
		)
		require.NoError(t, err)

		// Create authorization policy for ext-authz
		err = createAuthorizationPolicy(t, "ext-authz", "default", "httpbin", "ext-authz", "/headers")
		require.NoError(t, err)

		gatewayDomain, gatewayPort, err := portforward.CreateIngressGatewayPortForwarding(t)
		require.NoError(t, err)

		// Then: Verify requests work as expected
		// Test request to root path should return 200
		err = wait.For(func(ctx context.Context) (done bool, err error) {
			err = extauth.AssertEndpoint(
				t,
				http.MethodGet,
				fmt.Sprintf("http://%s:%d/", gatewayDomain, gatewayPort),
				map[string]string{"Host": "httpbin.default.svc.cluster.local"},
				http.StatusOK,
			)
			if err != nil {
				return false, err
			}
			return true, nil
		}, wait.WithTimeout(30*time.Second), wait.WithInterval(2*time.Second))
		require.NoError(t, err)

		// Test request to /headers with allow header should return 200
		err = extauth.AssertEndpoint(t, http.MethodGet, fmt.Sprintf("http://%s:%d/headers", gatewayDomain, gatewayPort),
			map[string]string{
				"Host":        "httpbin.default.svc.cluster.local",
				"x-ext-authz": "allow",
			}, http.StatusOK)
		require.NoError(t, err)

		// Verify ext-authz logs contain the expected values
		log.AssertContainerLogContains(t, c, "ext-authz", "ext-auth", "ext-authz", "X-Add-In-Check:[value] X-Ext-Authz:[allow]")

		// Test request to /headers with deny header should return 403
		err = extauth.AssertEndpoint(t, http.MethodGet, fmt.Sprintf("http://%s:%d/headers", gatewayDomain, gatewayPort),
			map[string]string{
				"Host":        "httpbin.default.svc.cluster.local",
				"x-ext-authz": "deny",
			}, http.StatusForbidden)
		require.NoError(t, err)
	})

	// Scenario: External authorizer (second variant with ext-authz2 provider)
	t.Run("External authorizer with ext-authz2 provider", func(t *testing.T) {
		// Given: Create namespace and enable istio injection
		err := infrahelpers.CreateNamespace(t, "default", infrahelpers.WithSidecarInjectionEnabled())
		require.NoError(t, err)

		// Deploy httpbin
		_, _, err = httpbin.DeployHttpbin(t, "default")
		require.NoError(t, err)

		c, err := client.ResourcesClient(t)
		require.NoError(t, err)

		// Wait for httpbin deployment to be ready
		httpbinDeployment := &v1.Deployment{}
		err = c.Get(t.Context(), "httpbin", "default", httpbinDeployment)
		require.NoError(t, err)
		err = wait.For(conditions.New(c).DeploymentConditionMatch(httpbinDeployment, v1.DeploymentAvailable, corev1.ConditionTrue), wait.WithContext(t.Context()))
		require.NoError(t, err)

		// Deploy ext-authz application
		err = extauthhelper.CreateExtAuth(t)
		require.NoError(t, err)

		// Create Istio CR with external authorizer (ext-authz2)
		authorizer := &v1alpha2.Authorizer{
			Name:    "ext-authz2",
			Port:    8000,
			Service: "ext-authz.ext-auth.svc.cluster.local",
		}
		_, err = modulehelpers.NewIstioCRBuilder().
			WithAuthorizer(authorizer).
			ApplyAndCleanup(t)
		require.NoError(t, err)

		// Create gateway and virtual service
		err = gatewayhelper.CreateHTTPGateway(t)
		require.NoError(t, err)

		err = virtualservice.CreateVirtualService(
			t,
			"httpbin",
			"default",
			"httpbin.default.svc.cluster.local",
			[]string{"httpbin.default.svc.cluster.local"},
			[]string{"default/test-gateway"},
		)
		require.NoError(t, err)

		// Create authorization policy for ext-authz2
		err = createAuthorizationPolicy(t, "ext-authz2", "default", "httpbin", "ext-authz2", "/headers")
		require.NoError(t, err)

		gatewayDomain, gatewayPort, err := portforward.CreateIngressGatewayPortForwarding(t)
		require.NoError(t, err)

		// Then: Verify requests work as expected
		// Test request to root path should return 200
		err = wait.For(func(ctx context.Context) (done bool, err error) {
			err = extauth.AssertEndpoint(
				t,
				http.MethodGet,
				fmt.Sprintf("http://%s:%d/", gatewayDomain, gatewayPort),
				map[string]string{"Host": "httpbin.default.svc.cluster.local"},
				http.StatusOK,
			)
			if err != nil {
				return false, err
			}
			return true, nil
		}, wait.WithTimeout(30*time.Second), wait.WithInterval(2*time.Second))
		require.NoError(t, err)

		// Test request to /headers with allow header should return 200
		err = extauth.AssertEndpoint(t, http.MethodGet, fmt.Sprintf("http://%s:%d/headers", gatewayDomain, gatewayPort),
			map[string]string{
				"Host":        "httpbin.default.svc.cluster.local",
				"x-ext-authz": "allow",
			}, http.StatusOK)
		require.NoError(t, err)

		// Verify ext-authz logs contain the expected values
		log.AssertContainerLogContains(t, c, "ext-authz", "ext-auth", "ext-authz", "X-Add-In-Check:[value] X-Ext-Authz:[allow]")

		// Test request to /headers with deny header should return 403
		err = extauth.AssertEndpoint(t, http.MethodGet, fmt.Sprintf("http://%s:%d/headers", gatewayDomain, gatewayPort),
			map[string]string{
				"Host":        "httpbin.default.svc.cluster.local",
				"x-ext-authz": "deny",
			}, http.StatusForbidden)
		require.NoError(t, err)

		// Verify ext-authz logs contain the expected values
		log.AssertContainerLogContains(t, c, "ext-authz", "ext-auth", "ext-authz", "X-Add-In-Check:[value] X-Ext-Authz:[deny]")
	})
}

// Helper function to assert proxy resources for a deployment
func assertProxyResourcesForDeployment(t *testing.T, c *resources.Resources, deploymentName, _ string, cpuRequest, memRequest, cpuLimit, memLimit string) {
	t.Helper()

	podList := &corev1.PodList{}
	err := c.List(t.Context(), podList, resources.WithLabelSelector(fmt.Sprintf("app=%s", deploymentName)))
	require.NoError(t, err)
	require.NotEmpty(t, podList.Items, "No pods found for deployment %s", deploymentName)

	for _, pod := range podList.Items {
		// Check init containers for istio-proxy
		for _, container := range pod.Spec.InitContainers {
			if container.Name == "istio-proxy" {
				assertResourceValues(t, container.Resources, cpuRequest, memRequest, cpuLimit, memLimit)
				return
			}
		}

		// Check regular containers for istio-proxy
		for _, container := range pod.Spec.Containers {
			if container.Name == "istio-proxy" {
				assertResourceValues(t, container.Resources, cpuRequest, memRequest, cpuLimit, memLimit)
				return
			}
		}
	}

	t.Fatalf("istio-proxy container not found in pods for deployment %s", deploymentName)
}

// Helper function to assert resource values
func assertResourceValues(t *testing.T, resources corev1.ResourceRequirements, cpuRequest, memRequest, cpuLimit, memLimit string) {
	t.Helper()

	// Assert CPU request
	actualCPURequest := resources.Requests[corev1.ResourceCPU]
	expectedCPURequest := resource.MustParse(cpuRequest)
	require.True(t, actualCPURequest.Equal(expectedCPURequest), "CPU request mismatch: expected %s, got %s", cpuRequest, actualCPURequest.String())

	// Assert Memory request
	actualMemRequest := resources.Requests[corev1.ResourceMemory]
	expectedMemRequest := resource.MustParse(memRequest)
	require.True(t, actualMemRequest.Equal(expectedMemRequest), "Memory request mismatch: expected %s, got %s", memRequest, actualMemRequest.String())

	// Assert CPU limit
	actualCPULimit := resources.Limits[corev1.ResourceCPU]
	expectedCPULimit := resource.MustParse(cpuLimit)
	require.True(t, actualCPULimit.Equal(expectedCPULimit), "CPU limit mismatch: expected %s, got %s", cpuLimit, actualCPULimit.String())

	// Assert Memory limit
	actualMemLimit := resources.Limits[corev1.ResourceMemory]
	expectedMemLimit := resource.MustParse(memLimit)
	require.True(t, actualMemLimit.Equal(expectedMemLimit), "Memory limit mismatch: expected %s, got %s", memLimit, actualMemLimit.String())
}

// Helper function to assert X-Envoy-External-Address header value
func assertEnvoyExternalAddress(t *testing.T, gatewayDomain string, gatewayPort int, xForwardedFor, expectedExternalAddress string) {
	t.Helper()

	err := wait.For(func(ctx context.Context) (done bool, err error) {
		// Make request with X-Forwarded-For header and check response for X-Envoy-External-Address
		url := fmt.Sprintf("http://%s:%d/headers", gatewayDomain, gatewayPort)
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return false, err
		}

		req.Header.Set("Host", "httpbin.default.svc.cluster.local")
		req.Header.Set("X-Forwarded-For", xForwardedFor)

		httpClient := &http.Client{}
		resp, err := httpClient.Do(req)
		if err != nil {
			return false, err
		}
		defer func() {
			_ = resp.Body.Close()
		}()

		if resp.StatusCode != http.StatusOK {
			return false, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
		}

		// Check for X-Envoy-External-Address in response headers
		actualExternalAddress := resp.Header.Get("X-Envoy-External-Address")
		if actualExternalAddress != expectedExternalAddress {
			return false, fmt.Errorf("X-Envoy-External-Address mismatch: expected %s, got %s", expectedExternalAddress, actualExternalAddress)
		}

		return true, nil
	}, wait.WithTimeout(30*time.Second), wait.WithInterval(2*time.Second))

	require.NoError(t, err)
}

// Helper function to create authorization policy for ext-authz
func createAuthorizationPolicy(t *testing.T, name, namespace, appSelector, provider, path string) error {
	t.Helper()
	t.Logf("Creating authorization policy %s in namespace %s", name, namespace)

	policy := fmt.Sprintf(`
apiVersion: security.istio.io/v1
kind: AuthorizationPolicy
metadata:
  name: %s
  namespace: %s
spec:
  action: CUSTOM
  provider:
    name: "%s"
  selector:
    matchLabels:
      app: %s
  rules:
    - to:
        - operation:
            paths: ["%s"]
`, name, namespace, provider, appSelector, path)

	_, err := infrahelpers.CreateResource(t, policy)
	if err != nil {
		t.Logf("Failed to create authorization policy: %v", err)
		return err
	}

	return nil
}
