package kubernetes

import (
	"context"
	"log/slog"

	"github.com/magnm/spale/pkg/kubernetes/client"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func OwningReplicaSet(pod *corev1.Pod) *appsv1.ReplicaSet {
	ownerReferences := pod.OwnerReferences
	if len(ownerReferences) == 0 {
		return nil
	}
	if ownerReferences[0].Kind != "ReplicaSet" {
		return nil
	}

	client, err := client.GetKubernetesClient()
	if err != nil {
		return nil
	}

	replicaSet, err := client.AppsV1().ReplicaSets(pod.Namespace).Get(context.Background(), ownerReferences[0].Name, metav1.GetOptions{})
	if err != nil {
		slog.Error("failed to get owning ReplicaSet", "error", err, "replicaSetName", ownerReferences[0].Name, "namespace", pod.Namespace)
		return nil
	}
	return replicaSet
}

func OwningDeployment(replicaSet *appsv1.ReplicaSet) *appsv1.Deployment {
	if replicaSet == nil {
		return nil
	}
	ownerReferences := replicaSet.OwnerReferences
	if len(ownerReferences) == 0 {
		return nil
	}
	if ownerReferences[0].Kind != "Deployment" {
		return nil
	}

	client, err := client.GetKubernetesClient()
	if err != nil {
		return nil
	}

	deployment, err := client.AppsV1().Deployments(replicaSet.Namespace).Get(context.Background(), ownerReferences[0].Name, metav1.GetOptions{})
	if err != nil {
		slog.Error("failed to get owning Deployment", "error", err, "deploymentName", ownerReferences[0].Name, "namespace", replicaSet.Namespace)
		return nil
	}
	return deployment
}
