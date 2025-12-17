package log

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/kyma-project/istio/operator/tests/e2e/pkg/helpers/client"
)

func StructToPrettyJson(t *testing.T, v interface{}) string {
	t.Helper()
	str, err := json.MarshalIndent(v, "", "    ")
	assert.NoError(t, err)
	return string(str)
}

func GetLogsFromIstioProxy(t *testing.T, podName, podNamespace string) ([]byte, error) {
	t.Helper()
	config := client.GetKubeConfig(t)
	k8sClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		t.Logf("Failed to create k8s client: %v", err)
		return nil, err
	}

	req := k8sClient.CoreV1().Pods(podNamespace).GetLogs(podName, &v1.PodLogOptions{
		Container: "istio-proxy",
	})

	logs, err := req.DoRaw(t.Context())
	if err != nil {
		t.Logf("Failed to get logs from istio-proxy container: %v", err)
		return nil, err
	}

	return logs, nil
}
