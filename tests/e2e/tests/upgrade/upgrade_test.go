package upgrade

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/e2e-framework/klient/k8s/resources"

	extauth "github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/gateway"
	"github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/virtual_service"

	"github.com/kyma-project/istio/operator/api/v1alpha2"
	"github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/crds"
	"github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/httpbin"
	"github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/modules"
	"github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/namespace"
	"github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/sidecar"

	"github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/client"
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
			"httpbin.default.svc.cluster.local",
			[]string{httpbinSvcName},
			[]string{"kyma-system/kyma-gateway"},
		)
		require.NoError(t, err)

		err = virtual_service.CreateVirtualService(
			t,
			"httpbin-vs-regular-sidecar",
			"default",
			"httpbin-regular-sidecar.default.svc.cluster.local",
			[]string{httpbinRegularSidecarSvcName},
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

		println("GOGO")
		time.Sleep(200 * time.Second)

	})

}
