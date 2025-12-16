package observability

import (
	_ "embed"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	alpha3 "istio.io/api/networking/v1alpha3"
	apitelemetryv1 "istio.io/api/telemetry/v1"
	"istio.io/client-go/pkg/apis/networking/v1alpha3"

	"github.com/kyma-project/istio/operator/tests/integration/steps"

	telemetryv1 "istio.io/client-go/pkg/apis/telemetry/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	"github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/httpbin"

	resourceClient "github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/client"
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

		tm := &telemetryv1.Telemetry{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "access-logs",
				Namespace: "istio-system",
			},
			Spec: apitelemetryv1.Telemetry{
				AccessLogging: []*apitelemetryv1.AccessLogging{
					{
						Providers: []*apitelemetryv1.ProviderRef{
							{Name: "stdout-json"},
						},
					},
				},
			},
		}

		cc, err := resourceClient.ResourcesClient(t)
		require.NoError(t, err)

		err = cc.Create(t.Context(), tm)
		require.NoError(t, err)

		cfg, err := config.GetConfig()
		require.NoError(t, err)
		c, err := client.New(cfg, client.Options{})
		require.NoError(t, err)

		ns := &v1.Namespace{}
		err = c.Get(t.Context(), client.ObjectKey{Name: "default"}, ns)
		require.NoError(t, err)
		cc.Label(ns, map[string]string{
			"istio-injection": "enabled",
		})
		err = cc.Update(t.Context(), ns)
		require.NoError(t, err)

		_, _, err = httpbin.DeployHttpbin(t, "default")
		require.NoError(t, err)

		time.Sleep(100 * time.Second)

		_, err = steps.CreateIstioGateway(t.Context(), "kyma-gateway", "kyma-system")
		require.NoError(t, err)

		vs := &v1alpha3.VirtualService{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "httpbin",
				Namespace: "istio-system",
			},
			Spec: alpha3.VirtualService{
				Gateways: []string{"kyma-system/kyma-gateway"},
				Hosts:    []string{"*"},
				Http: []*alpha3.HTTPRoute{
					{
						Name: "httpbin",
						Match: []*alpha3.HTTPMatchRequest{
							{
								Uri: &alpha3.StringMatch{
									MatchType: &alpha3.StringMatch_Exact{
										Exact: "/",
									},
								},
							},
						},
					},
				},
			},
		}
		err = cc.Create(t.Context(), vs)
		require.NoError(t, err)

		time.Sleep(200 * time.Second)

		//TODO: move that to helper function with the telemetry creation
		err = cc.Delete(t.Context(), tm)
		require.NoError(t, err)
		err = cc.Delete(t.Context(), vs)
		require.NoError(t, err)

	})
}
