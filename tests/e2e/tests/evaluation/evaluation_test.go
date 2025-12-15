package evaluation

import (
	_ "embed"
	"testing"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	"github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/httpbin"

	"github.com/kyma-project/istio/operator/api/v1alpha2"
	resourceClient "github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/client"
	modulehelpers "github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/modules"

	"github.com/kyma-project/istio/operator/internal/clusterconfig"
)

//go:embed istio_cr_default.yaml
var IstioDefault string

// CHANGE THE ORDER OF ASSERTIONS!!!
func TestEvaluationProfile(t *testing.T) {
	t.Run("Installation of Istio Module with evaluation profile", func(t *testing.T) {
		// given

		cfg, err := config.GetConfig()
		require.NoError(t, err)
		c, err := client.New(cfg, client.Options{})

		require.NoError(t, err)
		cs, err := clusterconfig.EvaluateClusterSize(t.Context(), c)
		require.NoError(t, err)
		require.Equal(t, clusterconfig.Evaluation.String(), cs.String())

		require.NoError(
			t,
			modulehelpers.CreateIstioOperatorCR(t,
				modulehelpers.WithIstioOperatorTemplate(IstioDefault),
			),
		)

		istioCR := &v1alpha2.Istio{}
		cc, err := resourceClient.ResourcesClient(t)
		require.NoError(t, err)
		err = cc.Get(t.Context(), "default", "kyma-system", istioCR)
		require.NoError(t, err)
		conditions := *istioCR.Status.Conditions
		require.Equal(t, v1alpha2.Ready, istioCR.Status.State)
		require.Equal(t, string(v1alpha2.ConditionReasonReconcileSucceeded), conditions[0].Reason)
		require.Equal(t, string(v1alpha2.ConditionTypeReady), conditions[0].Type)
		require.Equal(t, metav1.ConditionTrue, conditions[0].Status)

		ns := &v1.Namespace{}

		err = c.Get(t.Context(), client.ObjectKey{Name: "default"}, ns)
		require.NoError(t, err)
		cc.Label(ns, map[string]string{
			"istio-injection": "enabled",
		})
		err = cc.Update(t.Context(), ns)
		require.NoError(t, err)

		err = c.Get(t.Context(), client.ObjectKey{Name: "default"}, ns)
		require.NoError(t, err)

		_, _, err = httpbin.DeployHttpbin(t, "default")
		require.NoError(t, err)

		// istiod
		istiodPodList := &v1.PodList{}
		podListOpts := &client.ListOptions{
			Namespace: "istio-system",
			LabelSelector: labels.SelectorFromSet(map[string]string{
				"app": "istiod",
			}),
		}
		err = c.List(t.Context(), istiodPodList, podListOpts)
		require.NoError(t, err)

		for _, pod := range istiodPodList.Items {
			for _, container := range pod.Spec.InitContainers {
				require.Equal(t, "50m", container.Resources.Requests.Cpu().String())
				require.Equal(t, "128Mi", container.Resources.Requests.Memory().String())
				require.Equal(t, "1000m", container.Resources.Limits.Cpu().String())
				require.Equal(t, "1024Mi", container.Resources.Limits.Memory().String())
			}
		}

		// istio-ingressgateway
		igPodList := &v1.PodList{}
		podListOpts = &client.ListOptions{
			Namespace: "istio-system",
			LabelSelector: labels.SelectorFromSet(map[string]string{
				"app": "istio-ingressgateway",
			}),
		}
		err = c.List(t.Context(), igPodList, podListOpts)
		require.NoError(t, err)

		for _, pod := range igPodList.Items {
			for _, container := range pod.Spec.InitContainers {
				require.Equal(t, "10m", container.Resources.Requests.Cpu().String())
				require.Equal(t, "32Mi", container.Resources.Requests.Memory().String())
				require.Equal(t, "1000m", container.Resources.Limits.Cpu().String())
				require.Equal(t, "1024Mi", container.Resources.Limits.Memory().String())
			}
		}

	// workload
	httpbinPodList := &v1.PodList{}
	podListOpts = &client.ListOptions{
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
				require.Equal(t, "10m", container.Resources.Requests.Cpu().String())
				require.Equal(t, "32Mi", container.Resources.Requests.Memory().String())
				require.Equal(t, "250m", container.Resources.Limits.Cpu().String())
				require.Equal(t, "254Mi", container.Resources.Limits.Memory().String())
				break
			}
		}
	}
	require.True(t, containerFound)

	})
}
