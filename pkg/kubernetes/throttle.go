package kubernetes

import (
	"fmt"
	"slices"

	corev1 "k8s.io/api/core/v1"
	resourcehelper "k8s.io/component-helpers/resource"
	corev1helpers "k8s.io/component-helpers/scheduling/corev1"
	"k8s.io/component-helpers/scheduling/corev1/nodeaffinity"
)

type ErrNoNodesAvailable struct {
	Candidates int
	Limit      int
}

func (e *ErrNoNodesAvailable) Error() string {
	return fmt.Sprintf("all %d nodes with room for this pod have %d or more pods starting, try again later", e.Candidates, e.Limit)
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

// BusyCandidateNodes returns the nodes the pod could schedule on (and has room on) that already have limit or more pods starting.
func BusyCandidateNodes(pod *corev1.Pod, limit int) ([]string, error) {
	nodes, err := Nodes()
	if err != nil {
		return nil, err
	}
	pods, err := BoundPods()
	if err != nil {
		return nil, err
	}
	return busyCandidateNodes(pod, nodes, pods, limit)
}

type nodeUsage struct {
	requests corev1.ResourceList
	pods     int
	starting int
}

func busyCandidateNodes(pod *corev1.Pod, nodes []*corev1.Node, pods []*corev1.Pod, limit int) ([]string, error) {
	usage := map[string]*nodeUsage{}
	for _, p := range pods {
		u := usage[p.Spec.NodeName]
		if u == nil {
			u = &nodeUsage{requests: corev1.ResourceList{}}
			usage[p.Spec.NodeName] = u
		}
		u.pods++
		for name, q := range resourcehelper.PodRequests(p, resourcehelper.PodResourcesOptions{}) {
			total := u.requests[name]
			total.Add(q)
			u.requests[name] = total
		}
		if p.Labels[LabelManaged] == "true" && PodIsStarting(p) {
			u.starting++
		}
	}

	podRequests := resourcehelper.PodRequests(pod, resourcehelper.PodResourcesOptions{})
	candidates := 0
	var busy []string
	for _, node := range nodes {
		u := usage[node.Name]
		if u == nil {
			u = &nodeUsage{}
		}
		if !NodeFitsPod(pod, node) || !nodeHasRoom(node, u, podRequests) {
			continue
		}
		candidates++
		if u.starting >= limit {
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

func nodeHasRoom(node *corev1.Node, usage *nodeUsage, requests corev1.ResourceList) bool {
	allocatable := node.Status.Allocatable
	if maxPods, ok := allocatable[corev1.ResourcePods]; ok && int64(usage.pods+1) > maxPods.Value() {
		return false
	}
	for name, req := range requests {
		if req.IsZero() {
			continue
		}
		alloc, ok := allocatable[name]
		if !ok {
			return false
		}
		free := alloc.DeepCopy()
		free.Sub(usage.requests[name])
		if free.Cmp(req) < 0 {
			return false
		}
	}
	return true
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
