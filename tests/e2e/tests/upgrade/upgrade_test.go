package upgrade

import (
	"context"
	"fmt"
	"testing"

	httphelper "github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/http"
	"github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/load_balancer"
	"github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/zero_downtime"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/e2e-framework/klient/k8s/resources"
	"sigs.k8s.io/e2e-framework/klient/wait"

	"github.com/kyma-project/istio/operator/api/v1alpha2"
	"github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/client"
	"github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/crds"
	extauth "github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/gateway"
	"github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/httpbin"
	"github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/modules"
	"github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/namespace"
	"github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/sidecar"
	"github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/virtual_service"
)

func TestUpgrade(t *testing.T) {

	t.Run("Upgrade module version", func(t *testing.T) {
		c, err := client.ResourcesClient(t)
		require.NoError(t, err)

		err = modules.CreateIstioOperatorCR(t)
		require.NoError(t, err)

		err = crds.AssertIstioCRDsPresent(t.Context(), c.GetControllerRuntimeClient())
		require.NoError(t, err)

		i := v1alpha2.Istio{}
		err = c.Get(t.Context(), "default", "kyma-system", &i)
		require.NoError(t, err)

		err = namespace.LabelNamespaceWithIstioInjection(t, "default")
		require.NoError(t, err)

		httpbinSvcName, _, err := httpbin.DeployHttpbin(t, "default")
		require.NoError(t, err)
		httpbinRegularSidecarSvcName, _, err := httpbin.DeployHttpbinWithRegularSidecar(t, "default")
		require.NoError(t, err)

		err = extauth.CreateHTTPGateway(t)
		require.NoError(t, err)

		err = virtual_service.CreateVirtualService(
			t,
			"httpbin-vs",
			"default",
			httpbinSvcName,
			[]string{"httpbin.default.local.kyma.dev"},
			[]string{"kyma-system/kyma-gateway"},
		)
		require.NoError(t, err)

		err = virtual_service.CreateVirtualService(
			t,
			"httpbin-vs-regular-sidecar",
			"default",
			httpbinRegularSidecarSvcName,
			[]string{"httpbin-regular-sidecar.default.local.kyma.dev"},
			[]string{"kyma-system/kyma-gateway"},
		)
		require.NoError(t, err)

		httpbinPodList := &v1.PodList{}
		err = c.List(t.Context(), httpbinPodList, resources.WithLabelSelector("app=httpbin"))
		require.NoError(t, err)

		httpbinRegularPodList := &v1.PodList{}
		err = c.List(t.Context(), httpbinRegularPodList, resources.WithLabelSelector("app=httpbin-regular-sidecar"))
		require.NoError(t, err)

		for _, pod := range httpbinPodList.Items {
			err = sidecar.VerifyIfPodHasIstioSidecar(&pod)
			require.NoError(t, err)
		}

		for _, pod := range httpbinRegularPodList.Items {
			err = sidecar.VerifyIfPodHasIstioSidecar(&pod)
			require.NoError(t, err)
		}

		lbIp, err := load_balancer.GetLoadBalancerIP(t.Context(), c.GetControllerRuntimeClient())
		require.NoError(t, err)

		t.Logf("LoadBalancer IP: %s", lbIp)

		err = wait.For(func(ctx context.Context) (done bool, err error) {
			t.Logf("Waiting for endpoint to return 200 OK")
			httpClient := httphelper.NewHTTPClient(t,
				httphelper.WithPrefix("upgrade-test"),
				httphelper.WithHost("httpbin.default.local.kyma.dev"),
			)

			resp, err := httpClient.Get(fmt.Sprintf("http://%s/headers", lbIp))
			if err != nil {
				return false, nil
			}
			if resp.StatusCode != 200 {
				t.Logf("Endpoint status code %d", resp.StatusCode)
				return false, nil
			}

			return true, nil
		})
		require.NoError(t, err)

		// Start zero downtime testing for both httpbin endpoints
		t.Log("Starting zero downtime tests")
		zeroDowntimeRunner := &zero_downtime.ZeroDowntimeTestRunner{}

		_, err = zeroDowntimeRunner.StartZeroDowntimeTest(t.Context(), c.GetControllerRuntimeClient(), "httpbin.default.local.kyma.dev", "/headers")
		require.NoError(t, err)

		_, err = zeroDowntimeRunner.StartZeroDowntimeTest(t.Context(), c.GetControllerRuntimeClient(), "httpbin-regular-sidecar.default.local.kyma.dev", "/headers")
		require.NoError(t, err)

		// Perform the upgrade
		t.Log("Starting Istio module upgrade")
		err = modules.UpgradeIstioModule(t.Context(), c.GetControllerRuntimeClient())
		require.NoError(t, err)
		t.Log("Istio module upgrade completed successfully")

		// Stop zero downtime tests and verify no errors occurred
		t.Log("Stopping zero downtime tests and checking for errors")
		_, err = zeroDowntimeRunner.FinishZeroDowntimeTests(t.Context())
		require.NoError(t, err)
		t.Log("Zero downtime tests completed successfully - no downtime detected during upgrade")

	})

}
