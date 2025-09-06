package kubernetes

import (
	"context"

	"github.com/magnm/spale/pkg/kubernetes/client"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func ReplicaSetChildren(rs *appsv1.ReplicaSet, exceptPodName string) ([]corev1.Pod, error) {
	client, err := client.GetKubernetesClient()
	if err != nil {
		return nil, err
	}

	pods, err := client.CoreV1().Pods(rs.Namespace).List(context.Background(), metav1.ListOptions{
		LabelSelector: metav1.FormatLabelSelector(rs.Spec.Selector),
	})
	if err != nil {
		return nil, err
	}

	children := make([]corev1.Pod, 0, len(pods.Items))
	for _, pod := range pods.Items {
		if (pod.Status.Phase == "Running" || pod.Status.Phase == "Pending") && pod.Name != exceptPodName {
			children = append(children, pod)
		}
	}

	return children, nil
}
