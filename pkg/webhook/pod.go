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

	if rs := kubernetes.OwningReplicaSet(pod); rs != nil {
		annotations = &kubernetes.Annotations{}
		if deployment := kubernetes.OwningDeployment(rs); deployment != nil {
			annotations = kubernetes.DecodeAnnotations(deployment.Annotations)
		}

		if annotations.Ignore {
			slog.Debug("ignoring pod", "name", pod.Name, "namespace", pod.Namespace)
			return patches, nil
		}

		if !lo.Contains(config.Current.NamespaceSelector, "*") && !lo.Contains(config.Current.NamespaceSelector, pod.Namespace) {
			if annotations.OptIn {
				slog.Debug("namespace not in selector, but pod is opted-in", "name", pod.Name, "namespace", pod.Namespace)
			} else {
				slog.Debug("namespace not in selector, ignoring pod", "name", pod.Name, "namespace", pod.Namespace)
				return patches, nil
			}
		}
		if lo.Contains(config.Current.ExceptNamespaces, pod.Namespace) {
			if annotations.OptIn {
				slog.Debug("namespace in except list, but pod is opted-in", "name", pod.Name, "namespace", pod.Namespace)
			} else {
				slog.Debug("namespace in except list, ignoring pod", "name", pod.Name, "namespace", pod.Namespace)
				return patches, nil
			}
		}

		siblings, err = kubernetes.ReplicaSetChildren(rs, pod.Name)
		if err != nil {
			slog.Error("failed to get siblings of pod", "name", pod.Name, "namespace", pod.Namespace, "err", err)
			return nil, err
		}
	}

	if len(siblings) == 0 {
		slog.Debug("no siblings found for pod", "name", pod.Name, "namespace", pod.Namespace)
		return patches, nil
	}

	expectedNormal, _ := annotations.ExpectedCounts(len(siblings) + 1)
	currentSpot := lo.CountBy(siblings, annotations.PodIsSpot)
	currentNormal := len(siblings) - currentSpot

	if currentNormal < expectedNormal {
		slog.Debug("less than expected normal pods, keeping normal", "expectedNormal", expectedNormal, "currentNormal", currentNormal, "name", pod.Name, "namespace", pod.Namespace)
		return patches, nil
	}

	// Make sure affinity and tolerations top levels are present
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

	if dryRun {
		return []kubernetes.PatchOperation{}, nil
	}

	return patches, nil
}
