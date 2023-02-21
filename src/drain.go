package main

import (
	"github.com/aws/aws-sdk-go/aws/session"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"
	"k8s.io/kubectl/pkg/drain"
	"log"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	// DurationToConsiderNewNodes represents the time window to consider nodes joined to Kubernetes as new nodes
	// Default: since 10 minute ago
	DurationToConsiderNewNodes = -10 * time.Minute

	// Info messages
	WorkerLaunchedMessage  = "worker launched in background: draining the node: %s"
	DrainNotAllowedMessage = "drain is not allowed now, will be reviewed in the next loop"

	// Error messages
	DrainingErrorMessage        = "error draining the node '%s': %v"
	EventNotDeletedErrorMessage = "impossible to delete event from K8s: %v"
)

// DrainNodesOnRiskAuto TODO
func DrainNodesOnRiskAuto(client *kubernetes.Clientset, awsClient *session.Session, flags *ControllerFlags, eventPool *EventPool, nodePool *NodePool) {

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

	for {
		// Lock process on dry-run
		// TODO improve logging on dry-run
		if *flags.DryRun == true {
			log.Print(DrainNotAllowedMessage)
			time.Sleep(*flags.TimeBetweenDrains)
			continue
		}

		var waitGroup sync.WaitGroup

		// Check whether the eventPool is already filled by the watcher
		if len(eventPool.Events.Items) == 0 {
			time.Sleep(*flags.TimeBetweenDrains)
			continue
		}

		// Calculate recently added nodes, ignoring those with 'IgnoreRecentReadyNodeAnnotation' annotation
		recentlyAddedCount := GetRecentlyReadyNodeCountByNodeGroup(nodePool, DurationToConsiderNewNodes, true)
		groupedEvents := GetEventsByNodeGroup(eventPool, nodePool)

		// Loop over each nodegroup launching some drainage goroutines
		for nodegroupName, nodegroupReadyCount := range recentlyAddedCount {

			// No events for this nodegroup, jump
			if len(groupedEvents[nodegroupName]) == 0 {
				continue
			}

			// Get a batch of events from this nodegroup pool
			var currentMaxNumberDrainingEvents int
			var currentDrainingEvents []*v1.Event

			// Set the proper maximum drains for this nodegroup
			currentMaxNumberDrainingEvents = *flags.ConcurrentDrains
			if nodegroupReadyCount < *flags.ConcurrentDrains {
				currentMaxNumberDrainingEvents = nodegroupReadyCount
			}

			// Fewer events than allowed concurrent drains, get them all
			if len(groupedEvents[nodegroupName]) < currentMaxNumberDrainingEvents {
				currentDrainingEvents = groupedEvents[nodegroupName][0:len(groupedEvents[nodegroupName])]
			}

			// More events than allowed concurrent drains, get only the maximum allowed
			if len(groupedEvents[nodegroupName]) >= currentMaxNumberDrainingEvents {
				currentDrainingEvents = groupedEvents[nodegroupName][0:currentMaxNumberDrainingEvents]
			}

			for currentEventIndex, _ := range currentDrainingEvents {
				waitGroup.Add(1)
				// TODO annotate used ready nodes to avoid calculate with them again
				go DispatchDrainage(client, awsClient, drainHelper, nodePool, currentDrainingEvents[currentEventIndex], &waitGroup)
			}
		}

		waitGroup.Wait()

		time.Sleep(2 * time.Second)
	}
}

// DrainNodesOnRisk TODO
// TODO: Order events on the pool by AWS timestamp
func DrainNodesOnRisk(client *kubernetes.Clientset, awsClient *session.Session, flags *ControllerFlags, eventPool *EventPool, nodePool *NodePool, drainAllowed *bool) {

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

		// TODO Dry run??
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
			go DispatchDrainage(client, awsClient, drainHelper, nodePool, &currentDrainingEvents[currentEventIndex], &sync.WaitGroup{})
		}

		time.Sleep(*flags.TimeBetweenDrains)
	}
}

// DispatchDrainage drain a node according to data provided by an event
// This function is expected to be executed as a goroutine
func DispatchDrainage(client *kubernetes.Clientset, awsClient *session.Session, drainHelper *drain.Helper, nodePool *NodePool, event *v1.Event, waitGroup *sync.WaitGroup) {
	log.Printf(WorkerLaunchedMessage, event.InvolvedObject.Name) // TODO INFO
	err := drain.RunNodeDrain(drainHelper, event.InvolvedObject.Name)

	if err != nil {
		log.Printf(DrainingErrorMessage, event.InvolvedObject.Name, err)
	}

	// TODO should I terminate the instance?
	// providerID: aws:///eu-central-1a/i-042377dc1ee1257a1
	var providerIDSubstrings []string
	var instanceName string
	for _, node := range nodePool.Nodes.Items {
		if node.Name == event.InvolvedObject.Name {
			providerIDSubstrings = strings.Split(node.Spec.ProviderID, "/")
			log.Print(providerIDSubstrings[len(providerIDSubstrings)-1])
			instanceName = providerIDSubstrings[len(providerIDSubstrings)-1]
		}
	}

	err = AwsTerminateInstance(awsClient, instanceName)
	if err != nil && !errors.IsNotFound(err) {
		log.Printf("instance not found", err)
	}

	// Delete the event from Kubernetes
	err = DeleteKubernetesEvent(client, event.Namespace, event.Name)
	if err != nil && !errors.IsNotFound(err) {
		log.Printf(EventNotDeletedErrorMessage, err)
	}

	waitGroup.Done()
}
