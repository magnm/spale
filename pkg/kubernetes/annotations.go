package kubernetes

import (
	"math"
	"strconv"
	"strings"

	"github.com/magnm/spale/config"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
)

const (
	AnnotationRatio       string = "spale/ratio"
	AnnotationIgnore      string = "spale/ignore"
	AnnotationOptIn       string = "spale/opt-in"
	AnnotationNodeLabels  string = "spale/node-labels"
	AnnotationTolerations string = "spale/tolerations"
)

type Annotations struct {
	Ratio           string
	Ignore          bool
	OptIn           bool
	NodeLabels      []string
	NodeTolerations []string
}

func DecodeAnnotations(annotations map[string]string) *Annotations {
	if annotations == nil {
		return nil
	}
	return &Annotations{
		Ratio:           lo.CoalesceOrEmpty(annotations[AnnotationRatio], config.Current.SpotRatio),
		Ignore:          annotations[AnnotationIgnore] == "true",
		OptIn:           annotations[AnnotationOptIn] == "true",
		NodeLabels:      lo.CoalesceSliceOrEmpty(strings.Split(annotations[AnnotationNodeLabels], ","), config.Current.SpotNodeLabels),
		NodeTolerations: lo.CoalesceSliceOrEmpty(strings.Split(annotations[AnnotationTolerations], ","), config.Current.SpotNodeTolerations),
	}
}

func (a *Annotations) ExpectedCounts(currentTotal int) (int, int) {
	expectedRatio := a.Ratio
	if expectedRatio == "" {
		expectedRatio = "1:1"
	}
	parseRatio := func(ratio string) float64 {
		parts := strings.Split(ratio, ":")
		if len(parts) != 2 {
			return 1.0
		}
		numerator, errNum := strconv.ParseFloat(parts[0], 32)
		denominator, errDenom := strconv.ParseFloat(parts[1], 32)
		if errNum != nil || errDenom != nil || denominator == 0 {
			return 1.0
		}
		return numerator / (numerator + denominator)
	}

	ratio := parseRatio(expectedRatio)
	expectedNormal := math.Ceil(float64(currentTotal) * (1 - ratio))
	expectedSpot := currentTotal - int(expectedNormal)

	return int(expectedSpot), int(expectedNormal)
}

func (a *Annotations) NodeLabelPairs() map[string]string {
	labelPairs := make(map[string]string, len(a.NodeLabels))
	for _, label := range a.NodeLabels {
		parts := strings.Split(label, "=")
		if len(parts) >= 2 {
			labelPairs[parts[0]] = strings.Join(parts[1:], "=")
		} else if len(parts) == 1 {
			labelPairs[parts[0]] = "true"
		}
	}
	return labelPairs
}

func (a *Annotations) SpecAffinity() *corev1.NodeAffinity {
	if len(a.NodeLabels) == 0 {
		return nil
	}

	return &corev1.NodeAffinity{
		RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
			NodeSelectorTerms: []corev1.NodeSelectorTerm{
				{
					MatchExpressions: lo.MapToSlice(a.NodeLabelPairs(), func(key, value string) corev1.NodeSelectorRequirement {
						return corev1.NodeSelectorRequirement{
							Key:      key,
							Operator: corev1.NodeSelectorOpIn,
							Values:   []string{value},
						}
					}),
				},
			},
		},
	}
}

func (a *Annotations) SpecTolerations() []corev1.Toleration {
	if len(a.NodeTolerations) == 0 {
		return nil
	}

	tolerations := make([]corev1.Toleration, 0, len(a.NodeTolerations))
	for _, toleration := range a.NodeTolerations {
		parts := strings.SplitN(toleration, "=", 2)
		key := strings.TrimSpace(parts[0])
		valueParts := strings.SplitN(parts[1], ":", 2)
		operator := corev1.TolerationOpEqual
		if valueParts[0] == "" || valueParts[0] == "*" {
			operator = corev1.TolerationOpExists
			valueParts[0] = ""
		}
		effect := corev1.TaintEffect(valueParts[1])
		tolerations = append(tolerations, corev1.Toleration{
			Key:      key,
			Operator: operator,
			Value:    valueParts[0],
			Effect:   effect,
		})
	}
	return tolerations
}

func (a *Annotations) PodIsSpot(pod corev1.Pod) bool {
	if pod.Spec.Affinity == nil || pod.Spec.Affinity.NodeAffinity == nil || pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
		return false
	}

	nodeTerms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms

	for _, term := range nodeTerms {
		var matches = 0
		for key, value := range a.NodeLabelPairs() {
			for _, expr := range term.MatchExpressions {
				if expr.Key == key && expr.Operator == corev1.NodeSelectorOpIn && lo.Contains(expr.Values, value) {
					matches++
				}
			}
		}
		if matches == len(a.NodeLabels) {
			return true
		}
	}
	return false
}
