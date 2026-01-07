package zero_downtime

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"

	"github.com/cucumber/godog"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kyma-project/istio/operator/tests/integration/pkg/ip"
	"github.com/kyma-project/istio/operator/tests/integration/testsupport"

	"log"
	"net/http"
	"strings"
	"time"
)

const requestTimeout = 10 * time.Second
const testInterval = 500 * time.Millisecond
const numberOfThreads = 5

// ZeroDowntimeTestRunner holds Tester instances created by the StartZeroDowntimeTest function,
// so then the FinishZeroDowntimeTests function knows which Testers to stop
// Every test scenario instance should have own copy of this structure to allow parallel execution of tests
type ZeroDowntimeTestRunner struct {
	testers []testsupport.Tester
}

func getClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		Timeout: requestTimeout,
	}
}

func doSingleRequest(client *http.Client, host string, path string, lbUrl string) error {
	req, err := http.NewRequest(http.MethodGet, lbUrl, nil)
	if err != nil {
		return err
	}
	req.Host = host

	now := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request done at %s to host %s path %s failed because of: %w", now, host, path, err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("request done at %s to host %s path %s failed with status code: %d", now, host, path, resp.StatusCode)
	}

	time.Sleep(testInterval)
	return nil
}

func (zd *ZeroDowntimeTestRunner) StartZeroDowntimeTest(ctx context.Context, c client.Client, host string, path string) (context.Context, error) {
	ingressAddress, err := fetchIstioIngressGatewayAddress(ctx, c)
	if err != nil {
		return ctx, err
	}
	lbUrl := fmt.Sprintf("http://%s/%s", ingressAddress, strings.TrimLeft(path, "/"))
	client := getClient()

	testFn := func() error {
		return doSingleRequest(client, host, path, lbUrl)
	}

	tester := testsupport.NewTester(fmt.Sprintf("host-%s", host), testFn, numberOfThreads)
	zd.testers = append(zd.testers, tester)

	log.Printf("Starting zero downtime tester %s", tester.Name())
	tester.Start()

	return ctx, nil
}

func (zd *ZeroDowntimeTestRunner) FinishZeroDowntimeTests(ctx context.Context) (context.Context, error) {
	allErrs := make([]error, 0)

	for _, tester := range zd.testers {
		log.Printf("Stopping zero downtime tester %s", tester.Name())
		results := tester.Stop()
		for _, result := range results {
			if result.Err != nil {
				log.Printf("Got result from worker %s: requests: %d, error: %v", result.WorkerName, result.TestCount, result.Err)
				allErrs = append(allErrs, result.Err)
			} else {
				log.Printf("Got result from worker %s: requests: %d", result.WorkerName, result.TestCount)
			}
		}
	}

	if len(allErrs) > 0 {
		return ctx, fmt.Errorf("errors happened during zero downtime tests: %w", errors.Join(allErrs...))
	}

	return ctx, nil
}

func (zd *ZeroDowntimeTestRunner) CleanZeroDowntimeTests(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
	for _, tester := range zd.testers {
		log.Printf("Stopping zero downtime tester %s", tester.Name())
		tester.Stop()
	}
	return ctx, nil
}

func fetchIstioIngressGatewayAddress(ctx context.Context, c client.Client) (string, error) {
	istioIngressGatewayNamespaceName := types.NamespacedName{
		Name:      "istio-ingressgateway",
		Namespace: "istio-system",
	}

	var ingressIp string
	var ingressPort int32

	runsOnGardener, err := testsupport.RunsOnGardener(ctx, c)
	if err != nil {
		return "", err
	}

	if runsOnGardener {
		svc := corev1.Service{}
		if err := c.Get(ctx, istioIngressGatewayNamespaceName, &svc); err != nil {
			return "", err
		}

		if len(svc.Status.LoadBalancer.Ingress) == 0 {
			return "", errors.New("no ingress ip found")
		} else {
			lbIp, err := ip.GetLoadBalancerIp(svc.Status.LoadBalancer.Ingress[0])
			if err != nil {
				return "", err
			}

			ingressIp = lbIp.String()

			for _, port := range svc.Spec.Ports {
				if port.Name == "http2" {
					ingressPort = port.Port
				}
			}
		}
	} else {
		// In case we are not running on Gardener we assume that it's a k3d cluster that has 127.0.0.1 as default address
		ingressIp = "localhost"
		ingressPort = 80
	}

	return fmt.Sprintf("%s:%d", ingressIp, ingressPort), nil
}
