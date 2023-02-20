package main

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"
	"k8s.io/kubectl/pkg/drain"
	"log"
	"os"
	"time"
)

const (
	DrainTimeoutSeconds      = 120
	DrainConcurrentDrainages = 5

	DrainSynchronizationScheduleSeconds = 60
)

// DrainNodesOnRisk TODO
func DrainNodesOnRisk(client *kubernetes.Clientset, eventPool *EventPool, nodePool *NodePool) {

	// Prepare kubectl to drain nodes
	drainHelper := &drain.Helper{
		Client: client,
		Force:  true,

		GracePeriodSeconds: -1,

		IgnoreAllDaemonSets: true,
		Timeout:             time.Duration(DrainTimeoutSeconds) * time.Second,
		DeleteEmptyDirData:  true,

		Out:    os.Stdout,
		ErrOut: os.Stdout,
	}

	// Controlled loop to run workers
	for {

		// Check whether the eventPool is already filled by the watcher
		if len(eventPool.Events.Items) == 0 {
			time.Sleep(DrainSynchronizationScheduleSeconds * time.Second)
			continue
		}

		// Order events on the pool by AWS timestamp TODO

		// Get a batch of events from the pool.
		// Assumed first ones on the queue are on higher risk
		var currentDrainingEvents []v1.Event

		if len(eventPool.Events.Items) < DrainConcurrentDrainages {
			currentDrainingEvents = eventPool.Events.Items[0:len(eventPool.Events.Items)]
		} else {
			currentDrainingEvents = eventPool.Events.Items[0:DrainConcurrentDrainages]
		}

		for currentEventIndex, _ := range currentDrainingEvents {
			go DispatchDrainage(client, drainHelper, &currentDrainingEvents[currentEventIndex])
		}

		time.Sleep(DrainSynchronizationScheduleSeconds * time.Second)
	}
}

// DispatchDrainage drain a node according to data provided by an event
// This function is expected to be executed as a goroutine
func DispatchDrainage(client *kubernetes.Clientset, drainHelper *drain.Helper, event *v1.Event) {
	log.Printf("worker launched in background: draining the node: %s", event.InvolvedObject.Name)
	err := drain.RunNodeDrain(drainHelper, event.InvolvedObject.Name)

	if err != nil {
		log.Printf("error draining the node '%s': %v", event.InvolvedObject.Name, err)
	}

	// Delete the event from Kubernetes
	err = DeleteKubernetesEvent(client, event.Namespace, event.Name)
	if err != nil && !errors.IsNotFound(err) {
		log.Printf("impossible to delete event from K8s: %v", err)
	}
}
