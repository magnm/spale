package kubernetes

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/magnm/spale/pkg/kubernetes/client"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	corelisters "k8s.io/client-go/listers/core/v1"
)

const LabelManaged = "spale/managed"

var (
	managedPodLister corelisters.PodLister
	nodeLister       corelisters.NodeLister
)

func StartInformers(ctx context.Context) error {
	cs, err := client.GetKubernetesClient()
	if err != nil {
		return err
	}

	// Separate factories, since list option tweaks apply to every informer in a factory
	podFactory := informers.NewSharedInformerFactoryWithOptions(cs, 0,
		informers.WithTweakListOptions(func(o *metav1.ListOptions) {
			o.LabelSelector = LabelManaged + "=true"
		}),
		informers.WithTransform(trimPod),
	)
	nodeFactory := informers.NewSharedInformerFactoryWithOptions(cs, 0,
		informers.WithTransform(trimNode),
	)

	pods := podFactory.Core().V1().Pods()
	nodes := nodeFactory.Core().V1().Nodes()
	pods.Informer()
	nodes.Informer()

	podFactory.Start(ctx.Done())
	nodeFactory.Start(ctx.Done())

	syncCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	for typ, ok := range podFactory.WaitForCacheSync(syncCtx.Done()) {
		if !ok {
			return fmt.Errorf("failed to sync %v informer", typ)
		}
	}
	for typ, ok := range nodeFactory.WaitForCacheSync(syncCtx.Done()) {
		if !ok {
			return fmt.Errorf("failed to sync %v informer", typ)
		}
	}

	managedPodLister = pods.Lister()
	nodeLister = nodes.Lister()
	return nil
}

func ManagedPods() ([]*corev1.Pod, error) {
	if managedPodLister == nil {
		return nil, errors.New("pod informer not started")
	}
	return managedPodLister.List(labels.Everything())
}

func Nodes() ([]*corev1.Node, error) {
	if nodeLister == nil {
		return nil, errors.New("node informer not started")
	}
	return nodeLister.List(labels.Everything())
}

func trimPod(obj any) (any, error) {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return obj, nil
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              pod.Name,
			Namespace:         pod.Namespace,
			UID:               pod.UID,
			ResourceVersion:   pod.ResourceVersion,
			Labels:            pod.Labels,
			DeletionTimestamp: pod.DeletionTimestamp,
		},
		Spec: corev1.PodSpec{
			NodeName: pod.Spec.NodeName,
		},
		Status: corev1.PodStatus{
			Phase:      pod.Status.Phase,
			Conditions: pod.Status.Conditions,
		},
	}, nil
}

func trimNode(obj any) (any, error) {
	node, ok := obj.(*corev1.Node)
	if !ok {
		return obj, nil
	}
	node.ManagedFields = nil
	node.Status.Images = nil
	return node, nil
}
