package main

import (
	"context"
	"fmt"
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

// GetNodeList return a list of node resources
func GetNodeList(client *kubernetes.Clientset) (nodeList *v1.NodeList, err error) {

	nodeList, err = client.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})

	return nodeList, err
}

// GetKubernetesEventList return a list of event resources matching a reason
func GetKubernetesEventList(client *kubernetes.Clientset, eventReason string) (eventList *v1.EventList, err error) {

	eventList, err = client.CoreV1().Events("default").List(context.TODO(), metav1.ListOptions{
		FieldSelector: fmt.Sprintf("reason=%s", eventReason),
	})

	return eventList, err
}

// DeleteKubernetesEvent delete an event from the cluster
func DeleteKubernetesEvent(client *kubernetes.Clientset, namespace string, eventName string) (err error) {

	err = client.CoreV1().Events(namespace).Delete(context.TODO(), eventName, metav1.DeleteOptions{
		// DryRun: []string{"All"},
	})

	return err
}
