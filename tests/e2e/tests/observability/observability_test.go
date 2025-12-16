package observability

import (
	_ "embed"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/namespace"

	extauth "github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/gateway"
	"github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/telemetry"

	"github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/httpbin"

	modulehelpers "github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/modules"
)

//go:embed istio_cr_default.yaml
var IstioCR string

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

		_, _, err = httpbin.DeployHttpbin(t, "default")
		require.NoError(t, err)

		// create gateway
		err = extauth.CreateHTTPGateway(t)
		require.NoError(t, err)

		//vs := &v1alpha3.VirtualService{
		//	ObjectMeta: metav1.ObjectMeta{
		//		Name:      "httpbin",
		//		Namespace: "istio-system",
		//	},
		//	Spec: alpha3.VirtualService{
		//		Gateways: []string{"kyma-system/kyma-gateway"},
		//		Hosts:    []string{"*"},
		//		Http: []*alpha3.HTTPRoute{
		//			{
		//				Name: "httpbin",
		//				Match: []*alpha3.HTTPMatchRequest{
		//					{
		//						Uri: &alpha3.StringMatch{
		//							MatchType: &alpha3.StringMatch_Exact{
		//								Exact: "/",
		//							},
		//						},
		//					},
		//				},
		//			},
		//		},
		//	},
		//}
		//err = cc.Create(t.Context(), vs)
		//require.NoError(t, err)
		//println("TERAZ IDZ SPRAWDZ")
		//time.Sleep(100 * time.Second)

	})
}
