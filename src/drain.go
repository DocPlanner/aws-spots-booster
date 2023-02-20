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
	// Info messages
	WorkerLaunchedMessage  = "worker launched in background: draining the node: %s"
	DrainNotAllowedMessage = "drain is not allowed now, will be reviewed in the next loop"

	// Error messages
	DrainingErrorMessage        = "error draining the node '%s': %v"
	EventNotDeletedErrorMessage = "impossible to delete event from K8s: %v"
)

// DrainNodesOnRisk TODO
// TODO: Order events on the pool by AWS timestamp
func DrainNodesOnRisk(client *kubernetes.Clientset, flags *ControllerFlags, eventPool *EventPool, drainAllowed *bool) {

	// Prepare kubectl to drain nodes
	drainHelper := &drain.Helper{
		Client: client,
		Force:  true,

		GracePeriodSeconds: -1, // TODO: Decide the policy, 0, or -1??

		IgnoreAllDaemonSets: true,
		DeleteEmptyDirData:  true,
		Timeout:             *flags.DrainTimeout,

		Out:    os.Stdout,
		ErrOut: os.Stdout,
	}

	// Controlled loop to run workers
	for {
		if *drainAllowed == false {
			log.Print(DrainNotAllowedMessage)
			time.Sleep(*flags.TimeBetweenDrains)
			continue
		}

		// Check whether the eventPool is already filled by the watcher
		if len(eventPool.Events.Items) == 0 {
			time.Sleep(*flags.TimeBetweenDrains)
			continue
		}

		// Get a batch of events from the pool.
		// Assumed first ones on the queue are on higher risk
		var currentDrainingEvents []v1.Event

		if len(eventPool.Events.Items) < *flags.ConcurrentDrains {
			currentDrainingEvents = eventPool.Events.Items[0:len(eventPool.Events.Items)]
		} else {
			currentDrainingEvents = eventPool.Events.Items[0:*flags.ConcurrentDrains]
		}

		for currentEventIndex, _ := range currentDrainingEvents {
			go DispatchDrainage(client, drainHelper, &currentDrainingEvents[currentEventIndex])
		}

		time.Sleep(*flags.TimeBetweenDrains)
	}
}

// DispatchDrainage drain a node according to data provided by an event
// This function is expected to be executed as a goroutine
func DispatchDrainage(client *kubernetes.Clientset, drainHelper *drain.Helper, event *v1.Event) {
	log.Printf(WorkerLaunchedMessage, event.InvolvedObject.Name) // TODO INFO
	err := drain.RunNodeDrain(drainHelper, event.InvolvedObject.Name)

	if err != nil {
		log.Printf(DrainingErrorMessage, event.InvolvedObject.Name, err)
	}

	// Delete the event from Kubernetes
	err = DeleteKubernetesEvent(client, event.Namespace, event.Name)
	if err != nil && !errors.IsNotFound(err) {
		log.Printf(EventNotDeletedErrorMessage, err)
	}
}
