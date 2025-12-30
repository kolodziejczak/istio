package installation

import (
	_ "embed"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/inf.v0"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/e2e-framework/klient/k8s/resources"

	"github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/client"
	"github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/httpbin"
	modulehelpers "github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/modules"
	"github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/namespace"

	"github.com/kyma-project/istio/operator/tests/integration/pkg/crds"
)

//go:embed istio_cr_default.yaml
var IstioDefault string

//go:embed istio_cr_custom_resources.yaml
var IstioCustomResources string

func TestInstallation(t *testing.T) {
	t.Run("Installation of Istio module with default values", func(t *testing.T) {
		c, err := client.ResourcesClient(t)
		require.NoError(t, err)

		require.NoError(
			t,
			modulehelpers.CreateIstioOperatorCR(t,
				modulehelpers.WithIstioOperatorTemplate(IstioDefault),
			),
		)

		err = namespace.LabelNamespaceWithIstioInjection(t, "default")
		require.NoError(t, err)

		_, _, err = httpbin.DeployHttpbin(t, "default")
		require.NoError(t, err)

		// user workload
		httpbinPodList := &v1.PodList{}
		err = c.List(t.Context(), httpbinPodList, resources.WithLabelSelector("app=httpbin"))
		require.NoError(t, err)

		for _, pod := range httpbinPodList.Items {
			proxy := pod.Spec.InitContainers[1]
			require.Equal(t, "istio-proxy", proxy.Name)

			err = assertResources(resourceStruct{
				Cpu:    *proxy.Resources.Requests.Cpu(),
				Memory: *proxy.Resources.Requests.Memory(),
			}, "10m", "192Mi")
			require.NoError(t, err)

			err = assertResources(resourceStruct{
				Cpu:    *proxy.Resources.Limits.Cpu(),
				Memory: *proxy.Resources.Limits.Memory(),
			}, "1000m", "1024Mi")
			require.NoError(t, err)
		}

		// istio-ingressgateway
		ingressPodList := &v1.PodList{}
		err = c.List(t.Context(), ingressPodList, resources.WithLabelSelector("app=istio-ingressgateway"))
		require.NoError(t, err)

		for _, pod := range ingressPodList.Items {
			proxy := pod.Spec.Containers[0]
			require.Equal(t, "istio-proxy", proxy.Name)
			err = assertResources(resourceStruct{
				Cpu:    *proxy.Resources.Requests.Cpu(),
				Memory: *proxy.Resources.Requests.Memory(),
			}, "100m", "128Mi")
			require.NoError(t, err)

			err = assertResources(resourceStruct{
				Cpu:    *proxy.Resources.Limits.Cpu(),
				Memory: *proxy.Resources.Limits.Memory(),
			}, "2000m", "1024Mi")
			require.NoError(t, err)
		}

		// istiod
		istiodPodList := &v1.PodList{}
		err = c.List(t.Context(), istiodPodList, resources.WithLabelSelector("app=istiod"))
		require.NoError(t, err)
		for _, pod := range istiodPodList.Items {
			istiod := pod.Spec.Containers[0]
			require.Equal(t, "discovery", istiod.Name)
			err = assertResources(resourceStruct{
				Cpu:    *istiod.Resources.Requests.Cpu(),
				Memory: *istiod.Resources.Requests.Memory(),
			}, "100m", "512Mi")
			require.NoError(t, err)

			err = assertResources(resourceStruct{
				Cpu:    *istiod.Resources.Limits.Cpu(),
				Memory: *istiod.Resources.Limits.Memory(),
			}, "4000m", "2048Mi")
			require.NoError(t, err)
		}
	})
	t.Run("Installation of Istio module with custom values", func(t *testing.T) {
		//c, err := client.ResourcesClient(t)
		//require.NoError(t, err)

		require.NoError(
			t,
			modulehelpers.CreateIstioOperatorCR(t,
				modulehelpers.WithIstioOperatorTemplate(IstioCustomResources),
				modulehelpers.WithIstioOperatorTemplateValues(map[string]interface{}{
					"PilotCPULimit":        "1200m",
					"PilotMemoryLimit":     "1200Mi",
					"PilotCPURequests":     "15m",
					"PilotMemoryRequests":  "200Mi",
					"IGCPULimit":           "1500m",
					"IGMemoryLimit":        "1200Mi",
					"IGCPURequests":        "80m",
					"IGMemoryRequests":     "200Mi",
					"EgressGatewayEnabled": "true",
					"EGCPULimit":           "1400m",
					"EGMemoryLimit":        "1100Mi",
					"EGCPURequests":        "70m",
					"EGMemoryRequests":     "190Mi",
				}),
			),
		)

		// check if CRDS are in the cluster
		// create new kubernetes client.Client from "sigs.k8s.io/controller-runtime/pkg/client"
		cfg, err := config.GetConfig()
		require.NoError(t, err)
		c2, err := crclient.New(cfg, crclient.Options{})
		require.NoError(t, err)

		crdLister, err := crds.NewCRDListerFromFile(c2, "istio_crd_list.yaml")
		require.NoError(t, err)
		err = crdLister.EnsureCRDsArePresent(t.Context())
		require.NoError(t, err)

	})
}

type resourceStruct struct {
	Cpu    resource.Quantity
	Memory resource.Quantity
}

func assertResources(actualResources resourceStruct, expectedCpu, expectedMemory string) error {

	cpuMilli, err := strconv.Atoi(strings.TrimSuffix(expectedCpu, "m"))
	if err != nil {
		return err
	}

	memMilli, err := strconv.Atoi(strings.TrimSuffix(expectedMemory, "Mi"))
	if err != nil {
		return err
	}

	if resource.NewDecimalQuantity(*inf.NewDec(int64(cpuMilli), inf.Scale(resource.Milli)), resource.DecimalSI).Equal(actualResources.Cpu) {
		return fmt.Errorf("cpu wasn't expected; expected=%v got=%v", resource.NewScaledQuantity(int64(cpuMilli), resource.Milli), actualResources.Cpu)
	}

	if resource.NewDecimalQuantity(*inf.NewDec(int64(memMilli), inf.Scale(resource.Milli)), resource.DecimalSI).Equal(actualResources.Memory) {
		return fmt.Errorf("memory wasn't expected; expected=%v got=%v", resource.NewScaledQuantity(int64(memMilli), resource.Milli), actualResources.Memory)
	}

	return nil
}
