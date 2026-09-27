package kubernetes

import (
	"fmt"
	"slices"

	corev1 "k8s.io/api/core/v1"
	corev1helpers "k8s.io/component-helpers/scheduling/corev1"
	"k8s.io/component-helpers/scheduling/corev1/nodeaffinity"
)

type ErrNoNodesAvailable struct {
	Candidates int
	Limit      int
}

func (e *ErrNoNodesAvailable) Error() string {
	return fmt.Sprintf("all %d candidate nodes have %d or more pods starting, try again later", e.Candidates, e.Limit)
}

func PodIsStarting(pod *corev1.Pod) bool {
	if pod.Spec.NodeName == "" || pod.DeletionTimestamp != nil {
		return false
	}
	if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
		return false
	}
	for _, c := range pod.Status.Conditions {
		if c.Type == corev1.PodReady {
			return c.Status != corev1.ConditionTrue
		}
	}
	return true
}

func NodeFitsPod(pod *corev1.Pod, node *corev1.Node) bool {
	if node.Spec.Unschedulable || !nodeIsReady(node) {
		return false
	}
	if ok, err := nodeaffinity.GetRequiredNodeAffinity(pod).Match(node); err != nil || !ok {
		return false
	}
	_, untolerated := corev1helpers.FindMatchingUntoleratedTaint(node.Spec.Taints, pod.Spec.Tolerations, func(t *corev1.Taint) bool {
		return t.Effect == corev1.TaintEffectNoSchedule || t.Effect == corev1.TaintEffectNoExecute
	})
	return !untolerated
}

func nodeIsReady(node *corev1.Node) bool {
	for _, c := range node.Status.Conditions {
		if c.Type == corev1.NodeReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}

// BusyCandidateNodes returns the nodes the pod could schedule on that already have limit or more pods starting.
func BusyCandidateNodes(pod *corev1.Pod, limit int) ([]string, error) {
	nodes, err := Nodes()
	if err != nil {
		return nil, err
	}
	pods, err := ManagedPods()
	if err != nil {
		return nil, err
	}
	return busyCandidateNodes(pod, nodes, pods, limit)
}

func busyCandidateNodes(pod *corev1.Pod, nodes []*corev1.Node, pods []*corev1.Pod, limit int) ([]string, error) {
	starting := map[string]int{}
	for _, p := range pods {
		if PodIsStarting(p) {
			starting[p.Spec.NodeName]++
		}
	}

	candidates := 0
	var busy []string
	for _, node := range nodes {
		if !NodeFitsPod(pod, node) {
			continue
		}
		candidates++
		if starting[node.Name] >= limit {
			busy = append(busy, node.Name)
		}
	}

	// With no candidates at all, leave it to the scheduler (and autoscaler)
	if candidates > 0 && len(busy) == candidates {
		return nil, &ErrNoNodesAvailable{Candidates: candidates, Limit: limit}
	}
	slices.Sort(busy)
	return busy, nil
}

// WithExcludedNodes returns a copy of affinity that additionally requires the node not to be one of nodeNames.
func WithExcludedNodes(affinity *corev1.NodeAffinity, nodeNames []string) *corev1.NodeAffinity {
	if len(nodeNames) == 0 {
		return affinity
	}
	out := &corev1.NodeAffinity{}
	if affinity != nil {
		out = affinity.DeepCopy()
	}

	exclude := corev1.NodeSelectorRequirement{
		Key:      "metadata.name",
		Operator: corev1.NodeSelectorOpNotIn,
		Values:   nodeNames,
	}
	required := out.RequiredDuringSchedulingIgnoredDuringExecution
	if required == nil || len(required.NodeSelectorTerms) == 0 {
		out.RequiredDuringSchedulingIgnoredDuringExecution = &corev1.NodeSelector{
			NodeSelectorTerms: []corev1.NodeSelectorTerm{{MatchFields: []corev1.NodeSelectorRequirement{exclude}}},
		}
		return out
	}
	// Terms are ORed, so every term needs the exclusion
	for i := range required.NodeSelectorTerms {
		required.NodeSelectorTerms[i].MatchFields = append(required.NodeSelectorTerms[i].MatchFields, exclude)
	}
	return out
}
