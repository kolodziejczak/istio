package observability

import (
	_ "embed"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	infrahelpers "github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/infrastructure"

	"github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/namespace"

	extauth "github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/gateway"
	"github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/telemetry"

	"github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/httpbin"

	modulehelpers "github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/modules"
)

//go:embed istio_cr_default.yaml
var IstioCR string

//go:embed virtual_service.yaml
var VirtualService string

func TestObservability(t *testing.T) {

	t.Run("Logs from stdout-json envoyFileAccessLog provider are in correct format", func(t *testing.T) {
		require.NoError(
			t,
			modulehelpers.CreateIstioOperatorCR(t,
				modulehelpers.WithIstioOperatorTemplate(IstioCR),
			),
		)

		err := telemetry.EnableLogs(t)
		require.NoError(t, err)

		err = namespace.LabelNamespaceWithIstioInjection(t, "default")
		require.NoError(t, err)

		httpbinSvcName, httpbinPort, err := httpbin.DeployHttpbin(t, "default")
		require.NoError(t, err)

		// create gateway
		err = extauth.CreateHTTPGateway(t)
		require.NoError(t, err)

		// when
		createdVS, err := infrahelpers.CreateResourceWithTemplateValues(
			t,
			VirtualService,
			map[string]any{
				"Name":            "BOGDAN",
				"HostName":        "*",
				"DestinationHost": httpbinSvcName,
				"DestinationPort": httpbinPort,
				"GatewayName":     "kyma-system/kyma-gateway",
			},
		)
		require.NoError(t, err, "Failed to create VirtualService resource")
		require.NotEmpty(t, createdVS, "Created VirtualService resource should not be empty")

		println("TERAZ IDZ I SPRAWDZ")
		time.Sleep(100 * time.Second)
	})
}
