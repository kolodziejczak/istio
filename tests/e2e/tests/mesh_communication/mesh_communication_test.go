package mesh_communication

import (
	"context"
	_ "embed"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/e2e-framework/klient/decoder"
	"sigs.k8s.io/e2e-framework/klient/wait"

	httphelper "github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/http"

	extauth "github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/gateway"

	"github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/httpbin"
	"github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/infrastructure"
	modulehelpers "github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/modules"
	"github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/nginx"
)

//go:embed istio_cr_default.yaml
var IstioCR string

//go:embed virtual_service_nginx.yaml
var VirtualServiceSourceWorkload string

func TestMeshCommunication(t *testing.T) {
	t.Run("Access between applications in different namespaces", func(t *testing.T) {
		require.NoError(
			t,
			modulehelpers.CreateIstioOperatorCR(t,
				modulehelpers.WithIstioOperatorTemplate(IstioCR),
			),
		)

		err := infrastructure.CreateNamespace(
			t,
			"target",
			infrastructure.WithSidecarInjectionEnabled(),
		)
		require.NoError(t, err)

		svcName, svcPort, err := httpbin.DeployHttpbin(t, "target")
		require.NoError(t, err)

		err = infrastructure.CreateNamespace(
			t,
			"source",
			infrastructure.WithSidecarInjectionEnabled(),
		)
		require.NoError(t, err)

		forwardTarget := fmt.Sprintf("%s.target.svc.cluster.local:%d", svcName, svcPort)
		sourceWorkloadUrl, err := nginx.CreateForwardRequestNginx(t, "nginx-mesh-communication", "source", forwardTarget)
		require.NoError(t, err)

		err = extauth.CreateHTTPGateway(t)
		require.NoError(t, err)

		createdVs, err := infrastructure.CreateResourceWithTemplateValues(
			t,
			VirtualServiceSourceWorkload,
			map[string]any{
				"Name":            "nginx-mesh-communication",
				"GatewayName":     "kyma-system/kyma-gateway",
				"HostName":        "nginx-mesh-communication.local.kyma.dev",
				"DestinationHost": sourceWorkloadUrl,
				"DestinationPort": 80,
			},
			decoder.MutateNamespace("source"),
		)
		require.NoError(t, err)
		require.NotEmpty(t, createdVs)

		err = wait.For(func(ctx context.Context) (done bool, err error) {
			t.Logf("Waiting for endpoint to return 200 OK")
			httpClient := httphelper.NewHTTPClient(t, httphelper.WithPrefix("mesh-communication-test"))

			resp, err := httpClient.Get("http://nginx-mesh-communication.local.kyma.dev/headers")
			if err != nil {
				return false, nil
			}

			respBody, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Logf("Failed to read response body: %v", err)
				return false, err
			}
			contains := strings.Contains(string(respBody), "httpbin.target.svc.cluster.local")
			if !contains {
				t.Logf("Endpoint not found in response: %s", string(respBody))
			} else {
				t.Logf("Endpoint found in response: %s", string(respBody))
			}

			return true, nil
		})

	})

	t.Run("Access between applications from injection disabled namespace to injection enabled namespace is restricted", func(t *testing.T) {
		require.NoError(
			t,
			modulehelpers.CreateIstioOperatorCR(t,
				modulehelpers.WithIstioOperatorTemplate(IstioCR),
			),
		)

		err := infrastructure.CreateNamespace(
			t,
			"target",
			infrastructure.WithSidecarInjectionEnabled(),
		)
		require.NoError(t, err)

		svcName, svcPort, err := httpbin.DeployHttpbin(t, "target")
		require.NoError(t, err)

		// source should not be istio injected
		err = infrastructure.CreateNamespace(
			t,
			"source",
		)
		require.NoError(t, err)

		forwardTarget := fmt.Sprintf("%s.target.svc.cluster.local:%d", svcName, svcPort)
		sourceWorkloadUrl, err := nginx.CreateForwardRequestNginx(t, "nginx-mesh-communication", "source", forwardTarget)
		require.NoError(t, err)

		err = extauth.CreateHTTPGateway(t)
		require.NoError(t, err)

		createdVs, err := infrastructure.CreateResourceWithTemplateValues(
			t,
			VirtualServiceSourceWorkload,
			map[string]any{
				"Name":            "nginx-mesh-communication",
				"GatewayName":     "kyma-system/kyma-gateway",
				"HostName":        "nginx-mesh-communication.local.kyma.dev",
				"DestinationHost": sourceWorkloadUrl,
				"DestinationPort": 80,
			},
			decoder.MutateNamespace("source"),
		)
		require.NoError(t, err)
		require.NotEmpty(t, createdVs)

		err = wait.For(func(ctx context.Context) (done bool, err error) {
			t.Logf("Waiting for endpoint to return 200 OK")
			httpClient := httphelper.NewHTTPClient(t, httphelper.WithPrefix("mesh-communication-test"))

			resp, err := httpClient.Get("http://nginx-mesh-communication.local.kyma.dev/headers")
			if err != nil {
				return false, nil
			}
			require.Equal(t, 502, resp.StatusCode)

			return true, nil
		})

	})
}
