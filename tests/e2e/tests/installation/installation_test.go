package installation

import (
	_ "embed"
	"testing"

	"github.com/stretchr/testify/require"

	modulehelpers "github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/modules"
)

//go:embed istio_cr_default.yaml
var IstioDefault string

func TestInstallation(t *testing.T) {
	t.Run("Installation of Istio module with default values", func(t *testing.T) {
		require.NoError(
			t,
			modulehelpers.CreateIstioOperatorCR(t,
				modulehelpers.WithIstioOperatorTemplate(IstioDefault),
			),
		)

	})
}
