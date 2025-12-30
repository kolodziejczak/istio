package installation

import (
	_ "embed"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/inf.v0"
	v3 "istio.io/client-go/pkg/apis/security/v1"
	v2 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/e2e-framework/klient/decoder"
	"sigs.k8s.io/e2e-framework/klient/k8s"
	"sigs.k8s.io/e2e-framework/klient/k8s/resources"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/klient/wait/conditions"

	"github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/infrastructure"

	"github.com/kyma-project/istio/operator/api/v1alpha2"

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

//go:embed istio_cr_custom_name_namespace.yaml
var IstioCustomNameNamespace string

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
		c, err := client.ResourcesClient(t)
		require.NoError(t, err)

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

		istioNs := v1.Namespace{}
		err = c.Get(t.Context(), "istio-system", "", &istioNs)
		require.NoError(t, err)
		_, ok := istioNs.Annotations["istios.operator.kyma-project.io/managed-by-disclaimer"]
		require.True(t, ok, "istio-system namespace is not labeled with istios.operator.kyma-project.io/managed-by-disclaimer")
		_, ok = istioNs.Labels["namespaces.warden.kyma-project.io/validate"]
		require.True(t, ok, "istio-system namespace is not labeled with namespaces.warden.kyma-project.io/validate=true")

		// istiod is ready
		istiodDeployment := &v2.Deployment{}
		err = c.Get(t.Context(), "istiod", "istio-system", istiodDeployment)
		require.NoError(t, err)
		err = wait.For(conditions.New(c).DeploymentConditionMatch(istiodDeployment, v2.DeploymentAvailable, v1.ConditionTrue), wait.WithContext(t.Context()))

		// istio-ingressgateway is ready
		ingressDeployment := &v2.Deployment{}
		err = c.Get(t.Context(), "istio-ingressgateway", "istio-system", ingressDeployment)
		require.NoError(t, err)
		err = wait.For(conditions.New(c).DeploymentConditionMatch(ingressDeployment, v2.DeploymentAvailable, v1.ConditionTrue), wait.WithContext(t.Context()))
		require.NoError(t, err)

		// istio-egressgateway is ready
		egressDeployment := &v2.Deployment{}
		err = c.Get(t.Context(), "istio-egressgateway", "istio-system", egressDeployment)
		require.NoError(t, err)
		err = wait.For(conditions.New(c).DeploymentConditionMatch(egressDeployment, v2.DeploymentAvailable, v1.ConditionTrue), wait.WithContext(t.Context()))
		require.NoError(t, err)

		// istio-cni-node is ready
		cniDaemonSet := &v2.DaemonSet{}
		err = c.Get(t.Context(), "istio-cni-node", "istio-system", cniDaemonSet)
		require.NoError(t, err)
		err = wait.For(conditions.New(c).DaemonSetReady(cniDaemonSet), wait.WithContext(t.Context()))
		require.NoError(t, err)

		// ensure pilot limits and requests
		istiodPodList := &v1.PodList{}
		err = c.List(t.Context(), istiodPodList, resources.WithLabelSelector("app=istiod"))
		require.NoError(t, err)
		for _, pod := range istiodPodList.Items {
			istiod := pod.Spec.Containers[0]
			require.Equal(t, "discovery", istiod.Name)
			err = assertResources(resourceStruct{
				Cpu:    *istiod.Resources.Requests.Cpu(),
				Memory: *istiod.Resources.Requests.Memory(),
			}, "15m", "200Mi")
			require.NoError(t, err)

			err = assertResources(resourceStruct{
				Cpu:    *istiod.Resources.Limits.Cpu(),
				Memory: *istiod.Resources.Limits.Memory(),
			}, "1200m", "1200Mi")
			require.NoError(t, err)
		}

		// ensure ingressgateway limits and requests
		ingressPodList := &v1.PodList{}
		err = c.List(t.Context(), ingressPodList, resources.WithLabelSelector("app=istio-ingressgateway"))
		require.NoError(t, err)
		for _, pod := range ingressPodList.Items {
			ingress := pod.Spec.Containers[0]
			require.Equal(t, "istio-proxy", ingress.Name)
			err = assertResources(resourceStruct{
				Cpu:    *ingress.Resources.Requests.Cpu(),
				Memory: *ingress.Resources.Requests.Memory(),
			}, "80m", "200Mi")
			require.NoError(t, err)

			err = assertResources(resourceStruct{
				Cpu:    *ingress.Resources.Limits.Cpu(),
				Memory: *ingress.Resources.Limits.Memory(),
			}, "1500m", "1200Mi")
			require.NoError(t, err)
		}

		// ensure egressgateway limits and requests
		egressPodList := &v1.PodList{}
		err = c.List(t.Context(), egressPodList, resources.WithLabelSelector("app=istio-egressgateway"))
		require.NoError(t, err)
		for _, pod := range egressPodList.Items {
			egress := pod.Spec.Containers[0]
			require.Equal(t, "istio-proxy", egress.Name)
			err = assertResources(resourceStruct{
				Cpu:    *egress.Resources.Requests.Cpu(),
				Memory: *egress.Resources.Requests.Memory(),
			}, "70m", "190Mi")
			require.NoError(t, err)

			err = assertResources(resourceStruct{
				Cpu:    *egress.Resources.Limits.Cpu(),
				Memory: *egress.Resources.Limits.Memory(),
			}, "1400m", "1100Mi")
			require.NoError(t, err)
		}
	})

	t.Run("Managed Istio resources are present", func(t *testing.T) {
		ov := os.Getenv("OPERATOR_VERSION")
		if ov == "" {
			ov = "dev"
		}

		c, err := client.ResourcesClient(t)
		require.NoError(t, err)

		require.NoError(
			t,
			modulehelpers.CreateIstioOperatorCR(t,
				modulehelpers.WithIstioOperatorTemplate(IstioDefault),
			),
		)

		pa := v3.PeerAuthentication{}
		err = c.Get(t.Context(), "default", "istio-system", &pa)
		require.NoError(t, err)

		v, ok := pa.Labels["app.kubernetes.io/version"]
		require.True(t, ok, "Missing app.kubernetes.io/version label on PeerAuthentication")
		require.Equal(t, ov, v)
	})

	t.Run("Installation of Istio module with Istio CR in different namespace", func(t *testing.T) {
		c, err := client.ResourcesClient(t)
		require.NoError(t, err)

		icr, err := infrastructure.CreateResourceWithTemplateValues(
			t,
			IstioDefault,
			map[string]any{},
			decoder.MutateNamespace("default"),
		)

		require.NoError(t, err)

		istioCR := &v1alpha2.Istio{}
		err = c.Get(t.Context(), icr.GetName(), icr.GetNamespace(), istioCR)
		require.NoError(t, err)

		err = wait.For(conditions.New(c).ResourceMatch(istioCR, func(object k8s.Object) bool {
			istio := object.(*v1alpha2.Istio)
			ensureConditions := func() bool {
				for _, condition := range *istio.Status.Conditions {
					if condition.Type == string(v1alpha2.ConditionTypeReady) &&
						condition.Reason == string(v1alpha2.ConditionReasonReconcileFailed) &&
						condition.Status == "False" {
						return true
					}
				}
				return false
			}
			if istio.Status.State == v1alpha2.Error &&
				strings.Contains(istio.Status.Description, "Stopped Istio CR reconciliation: istio CR is not in kyma-system namespace") &&
				strings.Contains(istio.Status.Description, "Will not reconcile automatically") &&
				ensureConditions() {
				return true
			}
			return false
		}))

	})

	t.Run("Installation of Istio module with a second Istio CR in kyma-system namespace", func(t *testing.T) {
		c, err := client.ResourcesClient(t)
		require.NoError(t, err)

		icr, err := infrastructure.CreateResourceWithTemplateValues(
			t,
			IstioCustomNameNamespace,
			map[string]any{
				"Name":      "default",
				"Namespace": "kyma-system",
			},
		)
		require.NoError(t, err)

		istioCR := &v1alpha2.Istio{}
		err = c.Get(t.Context(), icr.GetName(), icr.GetNamespace(), istioCR)
		require.NoError(t, err)
		err = wait.For(conditions.New(c).ResourceMatch(istioCR, func(object k8s.Object) bool {
			istio := object.(*v1alpha2.Istio)
			if istio.Status.State == v1alpha2.Ready {
				return true
			}
			return false
		}))

		icr2, err := infrastructure.CreateResourceWithTemplateValues(
			t,
			IstioCustomNameNamespace,
			map[string]any{
				"Name":      "second-istio-cr",
				"Namespace": "kyma-system",
			},
		)
		require.NoError(t, err)

		secondIstioCR := &v1alpha2.Istio{}
		err = c.Get(t.Context(), icr2.GetName(), icr2.GetNamespace(), secondIstioCR)
		require.NoError(t, err)
		err = wait.For(conditions.New(c).ResourceMatch(secondIstioCR, func(object k8s.Object) bool {
			istio := object.(*v1alpha2.Istio)
			ensureConditions := func() bool {
				for _, condition := range *istio.Status.Conditions {
					if condition.Type == string(v1alpha2.ConditionTypeReady) &&
						condition.Reason == string(v1alpha2.ConditionReasonOlderCRExists) &&
						condition.Status == "False" {
						return true
					}
				}
				return false
			}

			if istio.Status.State == v1alpha2.Warning &&
				strings.Contains(istio.Status.Description, "Stopped Istio CR reconciliation: only Istio CR default in kyma-system reconciles the module") &&
				strings.Contains(istio.Status.Description, "Will not reconcile automatically") &&
				ensureConditions() {
				return true
			}

			return false
		}))
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
