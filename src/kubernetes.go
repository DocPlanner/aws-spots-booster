package main

import (
	"context"
	"golang.org/x/exp/maps"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// GetKubernetesClient Return a Kubernetes client configured to connect from inside or outside the cluster
func GetKubernetesClient(connectionMode string, kubeconfigPath string) (*kubernetes.Clientset, error) {
	var config *rest.Config
	var client *kubernetes.Clientset

	// Create configuration to connect from inside the cluster using Kubernetes mechanisms
	config, err := rest.InClusterConfig()

	// Create configuration to connect from outside the cluster, using kubectl
	if connectionMode == "kubectl" {
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	}

	// Check configuration errors in both cases
	if err != nil {
		return client, err
	}

	// Construct the client
	client, err = kubernetes.NewForConfig(config)
	return client, err
}

// KubernetesDeleteEvent delete an event from the cluster
func KubernetesDeleteEvent(client *kubernetes.Clientset, namespace string, eventName string) (err error) {

	err = client.CoreV1().Events(namespace).Delete(context.TODO(), eventName, metav1.DeleteOptions{
		// DryRun: []string{"All"},
	})

	return err
}

// KubernetesAnnotateNode add some annotations to a node
func KubernetesAnnotateNode(client *kubernetes.Clientset, node *v1.Node, annotations map[string]string) (err error) {

	// Merge annotations with existing ones
	maps.Copy(annotations, node.Annotations)
	node.SetAnnotations(annotations)

	// Update the object in the cluster
	_, err = client.CoreV1().Nodes().Update(context.TODO(), node, metav1.UpdateOptions{
		// DryRun: []string{"All"},
	})
	if err != nil {
		return err
	}

	return err
}
