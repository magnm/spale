package webhook

import (
	"log/slog"

	"github.com/magnm/spale/config"
	"github.com/magnm/spale/pkg/kubernetes"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
)

func patchesForPod(pod *corev1.Pod, dryRun bool) ([]kubernetes.PatchOperation, error) {
	var (
		patches     []kubernetes.PatchOperation
		siblings    []corev1.Pod
		annotations *kubernetes.Annotations
		err         error
	)
	logger := slog.With("pod", pod.Name, "namespace", pod.Namespace)

	if rs := kubernetes.OwningReplicaSet(pod); rs != nil {
		annotations = &kubernetes.Annotations{}
		if deployment := kubernetes.OwningDeployment(rs); deployment != nil {
			annotations = kubernetes.DecodeAnnotations(deployment.Annotations)
		}
		if pod.Name == "" && rs.Name != "" {
			logger = slog.With("pod", rs.Name, "namespace", pod.Namespace)
		}

		if annotations.Ignore {
			logger.Debug("ignoring pod")
			return patches, nil
		}

		if !lo.Contains(config.Current.NamespaceSelector, "*") && !lo.Contains(config.Current.NamespaceSelector, pod.Namespace) {
			if annotations.OptIn {
				logger.Debug("namespace not in selector, but pod is opted-in")
			} else {
				logger.Debug("namespace not in selector, ignoring pod")
				return patches, nil
			}
		}
		if lo.Contains(config.Current.ExceptNamespaces, pod.Namespace) {
			if annotations.OptIn {
				logger.Debug("namespace in except list, but pod is opted-in")
			} else {
				logger.Debug("namespace in except list, ignoring pod")
				return patches, nil
			}
		}

		siblings, err = kubernetes.ReplicaSetChildren(rs, pod.Name)
		if err != nil {
			logger.Error("failed to get siblings of pod", "err", err)
			return nil, err
		}
	}

	if len(siblings) == 0 {
		logger.Debug("no siblings found for pod")
		return patches, nil
	}

	currentTotal := len(siblings) + 1
	_, expectedNormal := annotations.ExpectedCounts(currentTotal)
	currentSpot := lo.CountBy(siblings, annotations.PodIsSpot)
	currentNormal := len(siblings) - currentSpot
	logger.Debug("pod siblings", "currentTotal", currentTotal, "currentNormal", currentNormal, "currentSpot", currentSpot, "ratio", annotations.Ratio, "expectedNormal", expectedNormal)

	deletionCost := 0
	// If there's a lot of normal pods, allow normal pods to be deleted with same
	// priority as spot. Otherwise spot gets lower cost.
	if float32(currentNormal) > float32(currentTotal)*0.2 {
		deletionCost = -1
	} else if currentNormal >= expectedNormal {
		deletionCost = -1
	}

	if deletionCost != 0 {
		logger.Debug("setting pod deletion cost", "deletionCost", deletionCost)
		if pod.Annotations == nil {
			patches = append(patches, kubernetes.PatchOperation{
				Op:    "add",
				Path:  "/metadata/annotations",
				Value: map[string]string{},
			})
		}
		patches = append(patches, kubernetes.PatchOperation{
			Op:    "add",
			Path:  "/metadata/annotations/controller.kubernetes.io~1pod-deletion-cost",
			Value: "-1",
		})
	}

	if dryRun {
		return []kubernetes.PatchOperation{}, nil
	}

	if currentNormal < expectedNormal {
		logger.Debug("less than expected normal pods, keeping normal", "expectedNormal", expectedNormal, "currentNormal", currentNormal)
		return patches, nil
	}

	// Make sure top levels are present
	if pod.Spec.Affinity == nil {
		patches = append(patches, kubernetes.PatchOperation{
			Op:    "add",
			Path:  "/spec/affinity",
			Value: &corev1.Affinity{},
		})
	}
	if pod.Spec.Tolerations == nil {
		patches = append(patches, kubernetes.PatchOperation{
			Op:    "add",
			Path:  "/spec/tolerations",
			Value: []corev1.Toleration{},
		})
	}

	// Set to spot
	patches = append(patches, kubernetes.PatchOperation{
		Op:    "add",
		Path:  "/spec/affinity/nodeAffinity",
		Value: annotations.SpecAffinity(),
	})
	patches = append(patches, kubernetes.PatchOperation{
		Op:    "add",
		Path:  "/spec/tolerations",
		Value: annotations.SpecTolerations(),
	})

	return patches, nil
}
