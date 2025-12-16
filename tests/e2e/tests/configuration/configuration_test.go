package configuration

import (
	_ "embed"
	"testing"

	"github.com/stretchr/testify/require"
	v2 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/klient/wait/conditions"

	resourceClient "github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/client"
	"github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/httpbin"

	modulehelpers "github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/modules"
)

//go:embed istio_cr_proxy_resources.yaml
var IstioCRProxyResource string

func TestConfiguration(t *testing.T) {
	//TODO: SHOULD WE EVALUATE IF THE CLUSTER IS IN PRODUCTION SIZE?
	t.Run("Updating proxy resource configuration (default - init sidecar)", func(t *testing.T) {
		require.NoError(
			t,
			modulehelpers.CreateIstioOperatorCR(t,
				modulehelpers.WithIstioOperatorTemplate(IstioCRProxyResource),
				//modulehelpers.WithIstioOperatorTemplateValues(map[string]interface{}{
				////"ProxyCPURequest": "30m",
				////"ProxyMemoryRequest": "190Mi",
				////"ProxyCPULimit": "700m",
				////"ProxyMemoryLimit": "700Mi",
				//}),
			),
		)
		cfg, err := config.GetConfig()
		require.NoError(t, err)
		c, err := client.New(cfg, client.Options{})
		require.NoError(t, err)

		cc, err := resourceClient.ResourcesClient(t)
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

		httpbinPodList := &v1.PodList{}
		podListOpts := &client.ListOptions{
			Namespace: "default",
			LabelSelector: labels.SelectorFromSet(map[string]string{
				"app": "httpbin",
			}),
		}
		err = c.List(t.Context(), httpbinPodList, podListOpts)
		require.NoError(t, err)

		containerFound := false
		for _, pod := range httpbinPodList.Items {
			for _, container := range pod.Spec.InitContainers {
				if container.Name == "istio-proxy" {
					containerFound = true
					//TODO: WE NEED TO CHANGE THIS TO PRODUCTION VALUES
					require.Equal(t, "10m", container.Resources.Requests.Cpu().String())
					require.Equal(t, "32Mi", container.Resources.Requests.Memory().String())
					require.Equal(t, "250m", container.Resources.Limits.Cpu().String())
					require.Equal(t, "254Mi", container.Resources.Limits.Memory().String())
					break
				}
			}
		}
		require.True(t, containerFound)

		err = modulehelpers.UpdateIstioOperatorCR(t,
			modulehelpers.WithIstioOperatorTemplate(IstioCRProxyResource),
			modulehelpers.WithIstioOperatorTemplateValues(map[string]interface{}{
				"ProxyCPURequest":    "30m",
				"ProxyMemoryRequest": "190Mi",
				"ProxyCPULimit":      "700m",
				"ProxyMemoryLimit":   "700Mi",
			}),
		)

		err = wait.For(conditions.New(cc).DeploymentAvailable("httpbin", "default"))
		require.NoError(t, err)

		httpbinPodList = &v1.PodList{}
		podListOpts = &client.ListOptions{
			Namespace: "default",
			LabelSelector: labels.SelectorFromSet(map[string]string{
				"app": "httpbin",
			}),
		}
		err = c.List(t.Context(), httpbinPodList, podListOpts)
		require.NoError(t, err)

		containerFound = false
		for _, pod := range httpbinPodList.Items {
			for _, container := range pod.Spec.InitContainers {
				if container.Name == "istio-proxy" {
					containerFound = true
					require.Equal(t, "30m", container.Resources.Requests.Cpu().String())
					require.Equal(t, "190Mi", container.Resources.Requests.Memory().String())
					require.Equal(t, "900m", container.Resources.Limits.Cpu().String())
					require.Equal(t, "900Mi", container.Resources.Limits.Memory().String())
					break
				}
			}
		}
		require.True(t, containerFound)
	})
	//TODO: t.Run("Updating proxy resource configuration (regular sidecar)", func(t *testing.T) {})

	t.Run("Egress Gateway has correct configuration", func(t *testing.T) {
		require.NoError(
			t,
			modulehelpers.CreateIstioOperatorCR(t,
				modulehelpers.WithIstioOperatorTemplate(IstioCRProxyResource),
				modulehelpers.WithIstioOperatorTemplateValues(map[string]interface{}{
					"EgressGatewayEnabled": "true",
				}),
			),
		)

		cfg, err := config.GetConfig()
		require.NoError(t, err)
		c, err := client.New(cfg, client.Options{})
		require.NoError(t, err)

		egressDeployment := &v2.Deployment{}
		err = c.Get(t.Context(), client.ObjectKey{Name: "istio-egressgateway", Namespace: "istio-system"}, egressDeployment)
		require.NoError(t, err)
	})

	//TODO: verify if external authorizer should be tested here as well
}
