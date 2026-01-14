package configuration

import (
	"context"
	"encoding/json"
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
	httphelper "github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/http"
	"github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/httpbin"
	infrahelpers "github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/infrastructure"
	"github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/load_balancer"
	"github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/log"
	modulehelpers "github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/modules"
	"github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/namespace"
	virtualservice "github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/virtual_service"
)

func TestConfiguration(t *testing.T) {
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

		err = modulehelpers.NewIstioCRBuilder().
			WithName(istioCR.Name).
			WithNamespace(istioCR.Namespace).
			WithProxyResources("80m", "230Mi", "900m", "900Mi").
			Update(t)
		require.NoError(t, err)

		assertProxyResourcesForDeployment(t, c, "httpbin", "default", "80m", "230Mi", "900m", "900Mi")
		assertProxyResourcesForDeployment(t, c, "httpbin-regular-sidecar", "default", "80m", "230Mi", "900m", "900Mi")
	})

	t.Run("Ingress Gateway adds correct X-Envoy-External-Address header after updating numTrustedProxies", func(t *testing.T) {
		c, err := client.ResourcesClient(t)
		require.NoError(t, err)

		istioCR, err := modulehelpers.NewIstioCRBuilder().
			WithNumTrustedProxies(1).
			ApplyAndCleanup(t)
		require.NoError(t, err)

		err = namespace.LabelNamespaceWithIstioInjection(t, "default")
		require.NoError(t, err)

		httpbinInfo, err := httpbin.NewBuilder().DeployWithCleanup(t)
		require.NoError(t, err)

		err = gatewayhelper.CreateHTTPGateway(t)
		require.NoError(t, err)

		err = virtualservice.CreateVirtualService(
			t,
			"test-vs",
			"default",
			httpbinInfo.Host,
			[]string{httpbinInfo.Host},
			[]string{"kyma-system/kyma-gateway"},
		)
		require.NoError(t, err)

		gatewayAddress, err := load_balancer.GetLoadBalancerIP(t.Context(), c.GetControllerRuntimeClient())
		require.NoError(t, err)

		assertEnvoyExternalAddress(t, gatewayAddress, httpbinInfo.Host, "10.2.1.1,10.0.0.1", "10.0.0.1")

		err = modulehelpers.NewIstioCRBuilder().
			WithName(istioCR.Name).
			WithNamespace(istioCR.Namespace).
			WithNumTrustedProxies(2).
			Update(t)
		require.NoError(t, err)

		assertEnvoyExternalAddress(t, gatewayAddress, httpbinInfo.Host, "10.2.1.1,10.0.0.1", "10.2.1.1")
	})

	t.Run("Egress Gateway has correct configuration", func(t *testing.T) {
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
		httpbinInfo, err := httpbin.NewBuilder().DeployWithCleanup(t)
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
			httpbinInfo.Host,
			[]string{httpbinInfo.Host},
			[]string{"default/test-gateway"},
		)
		require.NoError(t, err)

		// Create authorization policy for ext-authz
		err = createAuthorizationPolicy(t, "ext-authz", "default", "httpbin", "ext-authz", "/headers")
		require.NoError(t, err)

		gatewayAddress, err := load_balancer.GetLoadBalancerIP(t.Context(), c.GetControllerRuntimeClient())
		require.NoError(t, err)

		// Then: Verify requests work as expected
		// Test request to root path should return 200
		err = wait.For(func(ctx context.Context) (done bool, err error) {
			err = extauth.AssertEndpoint(
				t,
				http.MethodGet,
				fmt.Sprintf("http://%s/", gatewayAddress),
				map[string]string{"Host": httpbinInfo.Host},
				http.StatusOK,
			)
			if err != nil {
				return false, err
			}
			return true, nil
		}, wait.WithTimeout(30*time.Second), wait.WithInterval(2*time.Second))
		require.NoError(t, err)

		// Test request to /headers with allow header should return 200
		err = extauth.AssertEndpoint(t, http.MethodGet, fmt.Sprintf("http://%s/headers", gatewayAddress),
			map[string]string{
				"Host":        httpbinInfo.Host,
				"x-ext-authz": "allow",
			}, http.StatusOK)
		require.NoError(t, err)

		// Verify ext-authz logs contain the expected values
		log.AssertContainerLogContains(t, c, "ext-authz", "ext-auth", "ext-authz", "X-Add-In-Check:[value] X-Ext-Authz:[allow]")

		// Test request to /headers with deny header should return 403
		err = extauth.AssertEndpoint(t, http.MethodGet, fmt.Sprintf("http://%s/headers", gatewayAddress),
			map[string]string{
				"Host":        httpbinInfo.Host,
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
		httpbinInfo, err := httpbin.NewBuilder().DeployWithCleanup(t)
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
			httpbinInfo.Host,
			[]string{httpbinInfo.Host},
			[]string{"default/test-gateway"},
		)
		require.NoError(t, err)

		// Create authorization policy for ext-authz2
		err = createAuthorizationPolicy(t, "ext-authz2", "default", "httpbin", "ext-authz2", "/headers")
		require.NoError(t, err)

		gatewayAddress, err := load_balancer.GetLoadBalancerIP(t.Context(), c.GetControllerRuntimeClient())
		require.NoError(t, err)

		// Then: Verify requests work as expected
		// Test request to root path should return 200
		err = wait.For(func(ctx context.Context) (done bool, err error) {
			err = extauth.AssertEndpoint(
				t,
				http.MethodGet,
				fmt.Sprintf("http://%s/", gatewayAddress),
				map[string]string{"Host": httpbinInfo.Host},
				http.StatusOK,
			)
			if err != nil {
				return false, err
			}
			return true, nil
		}, wait.WithTimeout(30*time.Second), wait.WithInterval(2*time.Second))
		require.NoError(t, err)

		// Test request to /headers with allow header should return 200
		err = extauth.AssertEndpoint(t, http.MethodGet, fmt.Sprintf("http://%s/headers", gatewayAddress),
			map[string]string{
				"Host":        httpbinInfo.Host,
				"x-ext-authz": "allow",
			}, http.StatusOK)
		require.NoError(t, err)

		// Verify ext-authz logs contain the expected values
		log.AssertContainerLogContains(t, c, "ext-authz", "ext-auth", "ext-authz", "X-Add-In-Check:[value] X-Ext-Authz:[allow]")

		// Test request to /headers with deny header should return 403
		err = extauth.AssertEndpoint(t, http.MethodGet, fmt.Sprintf("http://%s/headers", gatewayAddress),
			map[string]string{
				"Host":        httpbinInfo.Host,
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

	// Wait for the deployment to be restarted with new resource configurations
	err := wait.For(func(ctx context.Context) (done bool, err error) {
		podList := &corev1.PodList{}
		err = c.List(ctx, podList, resources.WithLabelSelector(fmt.Sprintf("app=%s", deploymentName)))
		if err != nil {
			return false, err
		}

		if len(podList.Items) == 0 {
			return false, fmt.Errorf("no pods found for deployment %s", deploymentName)
		}

		for _, pod := range podList.Items {
			// Skip pods that are not ready
			if pod.Status.Phase != corev1.PodRunning {
				return false, nil
			}

			// Check init containers for istio-proxy
			for _, container := range pod.Spec.InitContainers {
				if container.Name == "istio-proxy" {
					if !checkResourceValues(container.Resources, cpuRequest, memRequest, cpuLimit, memLimit) {
						return false, nil
					}
					return true, nil
				}
			}

			// Check regular containers for istio-proxy
			for _, container := range pod.Spec.Containers {
				if container.Name == "istio-proxy" {
					if !checkResourceValues(container.Resources, cpuRequest, memRequest, cpuLimit, memLimit) {
						return false, nil
					}
					return true, nil
				}
			}
		}

		return false, fmt.Errorf("istio-proxy container not found in pods for deployment %s", deploymentName)
	}, wait.WithTimeout(1*time.Minute), wait.WithInterval(5*time.Second), wait.WithContext(t.Context()))

	require.NoError(t, err, "Failed to verify proxy resources for deployment %s", deploymentName)
}

// Helper function to check if resource values match expected values
func checkResourceValues(resources corev1.ResourceRequirements, cpuRequest, memRequest, cpuLimit, memLimit string) bool {
	expectedCPURequest := resource.MustParse(cpuRequest)
	expectedMemRequest := resource.MustParse(memRequest)
	expectedCPULimit := resource.MustParse(cpuLimit)
	expectedMemLimit := resource.MustParse(memLimit)

	actualCPURequest := resources.Requests[corev1.ResourceCPU]
	actualMemRequest := resources.Requests[corev1.ResourceMemory]
	actualCPULimit := resources.Limits[corev1.ResourceCPU]
	actualMemLimit := resources.Limits[corev1.ResourceMemory]

	return actualCPURequest.Equal(expectedCPURequest) &&
		actualMemRequest.Equal(expectedMemRequest) &&
		actualCPULimit.Equal(expectedCPULimit) &&
		actualMemLimit.Equal(expectedMemLimit)
}

// Helper function to assert X-Envoy-External-Address header value
func assertEnvoyExternalAddress(t *testing.T, gatewayAddress string, hostHeader string, xForwardedFor, expectedExternalAddress string) {
	t.Helper()

	httpClient := httphelper.NewHTTPClient(t, httphelper.WithHost(hostHeader))

	err := wait.For(func(ctx context.Context) (done bool, err error) {
		url := fmt.Sprintf("http://%s/headers", gatewayAddress)
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return false, err
		}

		req.Header.Set("X-Forwarded-For", xForwardedFor)

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

		// Parse JSON response body
		var bodyResponse struct {
			Headers map[string][]string `json:"headers"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&bodyResponse); err != nil {
			return false, fmt.Errorf("failed to decode response body: %w", err)
		}

		// Get X-Envoy-External-Address from the headers in the body
		externalAddressValues, ok := bodyResponse.Headers["X-Envoy-External-Address"]
		if !ok || len(externalAddressValues) == 0 {
			return false, fmt.Errorf("X-Envoy-External-Address not found in response body")
		}

		actualExternalAddress := externalAddressValues[0]
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
