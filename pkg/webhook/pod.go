package webhook

import (
	"log/slog"

	"github.com/magnm/spale/config"
	"github.com/magnm/spale/pkg/kubernetes"
	"github.com/samber/lo"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
)

func patchesForPod(pod *corev1.Pod, operation admissionv1.Operation, dryRun bool) ([]kubernetes.PatchOperation, error) {
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
	} else {
		annotations = kubernetes.DecodeAnnotations(pod.Annotations)
		if !annotations.Force {
			logger.Debug("pod not owned by a deployment, ignoring pod")
			return patches, nil
		}
		siblings = append(siblings, *pod)
	}

	// Pods with a preset nodeName bypass the scheduler, so node exclusion can't apply
	throttle := operation == admissionv1.Create && !dryRun && pod.Spec.NodeName == "" && config.Current.MaxStartingPodsPerNode > 0

	if len(siblings) == 0 {
		logger.Debug("no siblings found for pod")
		return schedulingPatches(logger, pod, patches, annotations, false, throttle)
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
		return schedulingPatches(logger, pod, patches, annotations, false, throttle)
	}

	return schedulingPatches(logger, pod, patches, annotations, true, throttle)
}

// schedulingPatches appends the node affinity/toleration patches, excluding nodes busy starting other pods if throttle is set.
func schedulingPatches(logger *slog.Logger, pod *corev1.Pod, patches []kubernetes.PatchOperation, annotations *kubernetes.Annotations, spot, throttle bool) ([]kubernetes.PatchOperation, error) {
	var nodeAffinity *corev1.NodeAffinity
	if pod.Spec.Affinity != nil {
		nodeAffinity = pod.Spec.Affinity.NodeAffinity
	}
	tolerations := pod.Spec.Tolerations
	if spot {
		nodeAffinity = annotations.SpecAffinity()
		tolerations = annotations.SpecTolerations()
	}

	var busyNodes []string
	if throttle {
		finalPod := pod.DeepCopy()
		if finalPod.Spec.Affinity == nil {
			finalPod.Spec.Affinity = &corev1.Affinity{}
		}
		finalPod.Spec.Affinity.NodeAffinity = nodeAffinity
		finalPod.Spec.Tolerations = tolerations

		var err error
		busyNodes, err = kubernetes.BusyCandidateNodes(finalPod, config.Current.MaxStartingPodsPerNode)
		if err != nil {
			return nil, err
		}
		if len(busyNodes) > 0 {
			logger.Debug("excluding nodes with too many starting pods", "nodes", busyNodes)
			nodeAffinity = kubernetes.WithExcludedNodes(nodeAffinity, busyNodes)
		}

		if pod.Labels == nil {
			patches = append(patches, kubernetes.PatchOperation{
				Op:    "add",
				Path:  "/metadata/labels",
				Value: map[string]string{},
			})
		}
		patches = append(patches, kubernetes.PatchOperation{
			Op:    "add",
			Path:  "/metadata/labels/spale~1managed",
			Value: "true",
		})
	}

	if !spot && len(busyNodes) == 0 {
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
	patches = append(patches, kubernetes.PatchOperation{
		Op:    "add",
		Path:  "/spec/affinity/nodeAffinity",
		Value: nodeAffinity,
	})

	if !spot {
		return patches, nil
	}

	// Set to spot
	if pod.Spec.Tolerations == nil {
		patches = append(patches, kubernetes.PatchOperation{
			Op:    "add",
			Path:  "/spec/tolerations",
			Value: []corev1.Toleration{},
		})
	}
	patches = append(patches, kubernetes.PatchOperation{
		Op:    "add",
		Path:  "/spec/tolerations",
		Value: tolerations,
	})

	return patches, nil
}
