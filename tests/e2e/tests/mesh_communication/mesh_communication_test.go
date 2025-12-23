package mesh_communication

import (
	_ "embed"
	"testing"

	"github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/httpbin"
	"github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/infrastructure"
	modulehelpers "github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/modules"
	"github.com/stretchr/testify/require"
)

//go:embed istio_cr_default.yaml
var IstioCR string

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

		_, _, err = httpbin.DeployHttpbin(t, "target")
		require.NoError(t, err)

		err = infrastructure.CreateNamespace(
			t,
			"source",
			infrastructure.WithSidecarInjectionEnabled(),
		)
		require.NoError(t, err)

		


	})

}