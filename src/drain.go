package main

import (
	"github.com/aws/aws-sdk-go/aws/session"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"
	"k8s.io/kubectl/pkg/drain"
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
	DrainingErrorMessage              = "error draining the node '%s': %v"
	EventNotDeletedErrorMessage       = "impossible to delete event from K8s: %v"
	InstanceNotFoundErrorMessage      = "instance '%s' not found. was it deleted by aws?: %v"
	UpdateNodeAnnotationsErrorMessage = "impossible to annotate a recently ready node '%s': %v"
)

// DrainNodesUnderRisk TODO
func DrainNodesUnderRisk(ctx *Ctx, client *kubernetes.Clientset, awsClient *session.Session, eventPool *EventPool, nodePool *NodePool) {

	// Prepare kubectl to drain nodes
	drainHelper := &drain.Helper{
		Client: client,
		Force:  true,

		GracePeriodSeconds: -1,

		IgnoreAllDaemonSets: true,
		DeleteEmptyDirData:  true,
		Timeout:             *ctx.Flags.DrainTimeout,

		Out:    os.Stdout,
		ErrOut: os.Stdout,
	}

	//
	if *ctx.Flags.IgnorePodsGracePeriod {
		drainHelper.GracePeriodSeconds = 0
	}

	for {
		// Lock process on dry-run
		if *ctx.Flags.DryRun == true {
			ctx.Logger.Info(DrainNotAllowedMessage)
			time.Sleep(*ctx.Flags.TimeBetweenDrains)
			continue
		}

		var waitGroup sync.WaitGroup

		// 1. Check whether the eventPool is already filled by the watcher
		if len(eventPool.Events.Items) == 0 {
			time.Sleep(*ctx.Flags.TimeBetweenDrains)
			continue
		}

		// Get recently added nodes, ignoring those with 'IgnoreRecentReadyNodeAnnotation' annotation
		recentlyAddedNodes := GetRecentlyReadyNodesByNodeGroup(nodePool, DurationToConsiderNewNodes, true)
		groupedEvents := GetEventsByNodeGroup(eventPool, nodePool)

		// 2. Loop over each nodegroup launching some drainage in parallel
		for nodegroupName, nodegroupNodes := range recentlyAddedNodes {

			nodegroupNodes = GetSortedNodeList(nodegroupNodes, true)
			nodegroupReadyCount := len(nodegroupNodes)

			// No events for this nodegroup, jump
			if len(groupedEvents[nodegroupName]) == 0 {
				continue
			}

			// Get a batch of events from this nodegroup pool
			var currentMaxNumberDrainingEvents int
			var currentDrainingEvents []*v1.Event

			// Set a maximum number of drains for this nodegroup
			currentMaxNumberDrainingEvents = *ctx.Flags.MaxConcurrentDrains
			if nodegroupReadyCount < *ctx.Flags.MaxConcurrentDrains {
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

				// Annotate a bare new Ready-node to avoid future drain calculations based on it
				err := KubernetesAnnotateNode(client, nodegroupNodes[currentEventIndex], map[string]string{
					IgnoreRecentReadyNodeAnnotation: IgnoreRecentReadyNodeAnnotationValue,
				})
				if err != nil {
					ctx.Logger.Infof(UpdateNodeAnnotationsErrorMessage, nodegroupNodes[currentEventIndex].Name, err)
				}

				// Execute a drain for a node under risk
				waitGroup.Add(1)
				go DispatchDrainage(ctx, client, awsClient, drainHelper, nodePool, currentDrainingEvents[currentEventIndex], &waitGroup)
			}
		}

		waitGroup.Wait()
		time.Sleep(*ctx.Flags.TimeBetweenDrains)
	}
}

// DispatchDrainage drain a node according to data provided by an event
// This function is expected to be executed as a goroutine
func DispatchDrainage(ctx *Ctx, client *kubernetes.Clientset, awsClient *session.Session, drainHelper *drain.Helper, nodePool *NodePool, event *v1.Event, waitGroup *sync.WaitGroup) {
	ctx.Logger.Infof(WorkerLaunchedMessage, event.InvolvedObject.Name) // TODO INFO
	err := drain.RunNodeDrain(drainHelper, event.InvolvedObject.Name)

	if err != nil {
		ctx.Logger.Infof(DrainingErrorMessage, event.InvolvedObject.Name, err)
	}

	// Terminate the problematic instance
	// Example of field: providerID: aws:///eu-central-1a/i-042377dc1ee1257a1
	var providerIDSubstrings []string
	var instanceName string
	for _, node := range nodePool.Nodes.Items {
		if node.Name == event.InvolvedObject.Name {
			providerIDSubstrings = strings.Split(node.Spec.ProviderID, "/")
			ctx.Logger.Info(providerIDSubstrings[len(providerIDSubstrings)-1])
			instanceName = providerIDSubstrings[len(providerIDSubstrings)-1]
		}
	}

	err = AwsTerminateInstance(awsClient, instanceName)
	if err != nil && !errors.IsNotFound(err) {
		ctx.Logger.Infof(InstanceNotFoundErrorMessage, instanceName, err)
	}

	// Delete the event from Kubernetes
	err = KubernetesDeleteEvent(client, event.Namespace, event.Name)
	if err != nil && !errors.IsNotFound(err) {
		ctx.Logger.Infof(EventNotDeletedErrorMessage, err)
	}

	waitGroup.Done()
}
