package pods

import (
	"context"

	"github.com/go-logr/logr"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/fields"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kyma-project/istio/operator/internal/restarter/predicates"
	"github.com/kyma-project/istio/operator/pkg/lib/sidecars/retry"
)

const (
	istioSidecarContainerName string = "istio-proxy"
)

type RestartLimits struct {
	PodsPerPage int
}

func NewPodsRestartLimits(podsPerPage int) *RestartLimits {
	return &RestartLimits{
		PodsPerPage: podsPerPage,
	}
}

type Getter interface {
	GetPodsToRestart(ctx context.Context, preds []predicates.SidecarProxyPredicate, limits *RestartLimits) (*v1.PodList, error)
	GetAllInjectedPods(context context.Context) (*v1.PodList, error)
}

type Pods struct {
	k8sClient client.Client
	logger    *logr.Logger
}

func NewPods(k8sClient client.Client, logger *logr.Logger) *Pods {
	return &Pods{
		k8sClient: k8sClient,
		logger:    logger,
	}
}

func (p *Pods) GetPodsToRestart(ctx context.Context, preds []predicates.SidecarProxyPredicate, limits *RestartLimits) (*v1.PodList, error) {
	// Page through all sidecar pods within a single reconciliation, collecting every matching pod.
	// All pagination is handled internally so that continue tokens never span across
	// reconciliation cycles (Kubernetes continue tokens expire after ~5 minutes).
	podsToRestart := &v1.PodList{}
	continueToken := ""

	for {
		podsWithSidecar, err := getSidecarPods(ctx, p.k8sClient, p.logger, limits.PodsPerPage, continueToken)
		if err != nil {
			if k8serrors.IsGone(err) {
				p.logger.Info("Continue token expired, restarting pod scan from the beginning")
				continueToken = ""
				continue
			}
			return nil, err
		}

		podsToRestart.Items = append(podsToRestart.Items, collectMatchingPods(podsWithSidecar.Items, preds)...)
		continueToken = podsWithSidecar.Continue

		if continueToken == "" {
			break
		}
	}

	if len(podsToRestart.Items) > 0 {
		p.logger.Info("Pods to restart", "number of pods", len(podsToRestart.Items))
	} else {
		p.logger.Info("No pods to restart with matching predicates")
	}

	return podsToRestart, nil
}

// matchesPredicate returns true if the pod satisfies all MustMatch predicates and at least one optional predicate.
func matchesPredicate(pod v1.Pod, preds []predicates.SidecarProxyPredicate) bool {
	optionalMatched := false
	for _, predicate := range preds {
		matched := predicate.Matches(pod)
		if predicate.MustMatch() {
			// All MustMatch predicates must evaluate to true.
			if !matched {
				return false
			}
		} else if matched {
			// At least one optional predicate must evaluate to true.
			optionalMatched = true
		}
	}
	return optionalMatched
}

// collectMatchingPods returns all pods from candidates that satisfy the given predicates.
func collectMatchingPods(candidates []v1.Pod, preds []predicates.SidecarProxyPredicate) []v1.Pod {
	var matched []v1.Pod
	for _, pod := range candidates {
		if matchesPredicate(pod, preds) {
			matched = append(matched, pod)
		}
	}
	return matched
}

func (p *Pods) GetAllInjectedPods(ctx context.Context) (*v1.PodList, error) {
	podList := &v1.PodList{}
	outputPodList := &v1.PodList{}
	outputPodList.Items = make([]v1.Pod, len(podList.Items))

	err := retry.OnError(retry.DefaultRetry, func() error {
		return p.k8sClient.List(ctx, podList, &client.ListOptions{})
	})
	if err != nil {
		return podList, err
	}

	for _, pod := range podList.Items {
		if containsSidecar(pod) {
			outputPodList.Items = append(outputPodList.Items, pod)
		}
	}

	return outputPodList, nil
}

func listRunningPods(ctx context.Context, c client.Client, listLimit int, continueToken string) (*v1.PodList, error) {
	podList := &v1.PodList{}

	err := retry.OnError(retry.DefaultRetry, func() error {
		listOps := []client.ListOption{
			client.MatchingFieldsSelector{Selector: fields.OneTermEqualSelector("status.phase", string(v1.PodRunning))},
			client.Limit(listLimit),
		}
		if continueToken != "" {
			listOps = append(listOps, client.Continue(continueToken))
		}
		return c.List(ctx, podList, listOps...)
	})

	return podList, err
}

func getSidecarPods(ctx context.Context, c client.Client, logger *logr.Logger, listLimit int, continueToken string) (*v1.PodList, error) {
	podList, err := listRunningPods(ctx, c, listLimit, continueToken)
	if err != nil {
		return nil, err
	}

	logger.Info("Got running pods for proxy restart", "number of pods", len(podList.Items), "has more pods", podList.Continue != "")

	podsWithSidecar := &v1.PodList{}
	podsWithSidecar.Continue = podList.Continue

	for _, pod := range podList.Items {
		if predicates.IsReadyWithIstioAnnotation(pod) {
			podsWithSidecar.Items = append(podsWithSidecar.Items, pod)
		}
	}

	logger.Info("Filtered pods with Istio sidecar", "number of pods", len(podsWithSidecar.Items))
	return podsWithSidecar, nil
}

func containsSidecar(pod v1.Pod) bool {
	// Exclude pods with label istio=ingressgateway or istio=egressgateway
	// These pods are not meant to be restarted by this part of the code
	// This function is used only for restart of the the user workloads during uninstalling Istio, so the sidecars are removed
	if val, ok := pod.Labels["istio"]; ok && (val == "ingressgateway" || val == "egressgateway") {
		return false
	}
	for _, container := range pod.Spec.Containers {
		if container.Name == istioSidecarContainerName {
			return true
		}
	}
	for _, initContainer := range pod.Spec.InitContainers {
		if initContainer.Name == istioSidecarContainerName {
			return true
		}
	}
	return false
}
