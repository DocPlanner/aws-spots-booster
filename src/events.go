package main

import (
	"context"
	"fmt"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"math"
	"strings"
	"time"
)

const (

	// Event reasons
	RebalanceEvent = "RebalanceRecommendation"

	// Info messages
	NodeChangedMessage            = "node change detected on '%s', checking the node pool"
	EventChangedMessage           = "event change detected on '%s/%s', checking the event pool"
	EventNotDeletedFromK8sMessage = "impossible to delete the event from kubernetes"
	ParseNotPossibleMessage       = "impossible to parse date on the message"
	DeleteOldEventMessage         = "An event is too old (%s), deleting: %s/%s"

	//
	WatchersLoopTime = 2 * time.Second
)

// WatchNodes watches for nodes on k8s and keep a pool up-to-date with them
// Done this way to reduce the calls done to Kube API
// This function must be executed as a go routine
func WatchNodes(ctx *Ctx, client *kubernetes.Clientset, nodePool *NodePool) {

	// Ensure retry to create a watcher when failing
	for {

		// Something failed, reset the pool
		nodePool.Lock.Lock()
		nodePool.Nodes = v1.NodeList{}
		nodePool.Lock.Unlock()

		nodesWatcher, err := client.CoreV1().Nodes().Watch(context.TODO(), metav1.ListOptions{})
		if err != nil {
			ctx.Logger.Info(err)
		}

		for event := range nodesWatcher.ResultChan() {

			nodeObject := event.Object.(*v1.Node)

			ctx.Logger.Infof(NodeChangedMessage, nodeObject.Name) // TODO INFO

			nodePool.Lock.Lock()

			switch event.Type {
			case watch.Added:
				nodePool.Nodes.Items = append(nodePool.Nodes.Items, *nodeObject)

			// Substitute previous with it
			case watch.Modified:
				for storedNodeIndex, storedNode := range nodePool.Nodes.Items {
					if nodeObject.Name == storedNode.Name {
						nodePool.Nodes.Items[storedNodeIndex] = *nodeObject
						break
					}
				}
			// Remove it from the pool: approach is last item to current position, then delete last
			case watch.Deleted:
				for storedNodeIndex, storedNode := range nodePool.Nodes.Items {
					if nodeObject.Name == storedNode.Name {
						nodePool.Nodes.Items[storedNodeIndex] = nodePool.Nodes.Items[len(nodePool.Nodes.Items)-1]
						nodePool.Nodes.Items = nodePool.Nodes.Items[:len(nodePool.Nodes.Items)-1]
						break
					}
				}
			}
			nodePool.Lock.Unlock()
		}

		time.Sleep(WatchersLoopTime)
	}
}

// WatchEvents watches for some reasoned events on k8s and keep a pool up-to-date with them
// This function must be executed as a go routine
func WatchEvents(ctx *Ctx, client *kubernetes.Clientset, eventReason string, eventPool *EventPool) {

	// Ensure retry to create a watcher when failing
	for {

		// Something failed, reset the pool
		eventPool.Lock.Lock()
		eventPool.Events = v1.EventList{}
		eventPool.Lock.Unlock()

		eventWatcher, err := client.CoreV1().Events("default").Watch(context.TODO(), metav1.ListOptions{
			FieldSelector: fmt.Sprintf("reason=%s", eventReason),
		})

		if err != nil {
			ctx.Logger.Info(err)
		}

		for event := range eventWatcher.ResultChan() {

			eventObject := event.Object.(*v1.Event)

			ctx.Logger.Infof(EventChangedMessage, eventObject.Namespace, eventObject.Name)

			eventPool.Lock.Lock()

			switch event.Type {
			case watch.Added:

				// Filter repeated events coming from same nodes. New will replace the old
				for storedEventIndex, storedEvent := range eventPool.Events.Items {
					if eventObject.InvolvedObject.Name == storedEvent.InvolvedObject.Name {
						eventPool.Events.Items[storedEventIndex] = *eventObject
						break
					}
				}

				// Not found, store it
				eventPool.Events.Items = append(eventPool.Events.Items, *eventObject)

			case watch.Deleted:
				// Remove the event from the pool: last item to current position, then delete last
				for storedEventIndex, storedEvent := range eventPool.Events.Items {

					if eventObject.InvolvedObject.Name == storedEvent.InvolvedObject.Name {
						eventPool.Events.Items[storedEventIndex] = eventPool.Events.Items[len(eventPool.Events.Items)-1]
						eventPool.Events.Items = eventPool.Events.Items[:len(eventPool.Events.Items)-1]
						break
					}
				}
			}

			eventPool.Lock.Unlock()
		}

		time.Sleep(WatchersLoopTime)
	}
}

// CleanKubernetesEvents delete old/nosense events from Kubernetes
// This function must be executed as a go routine
func CleanKubernetesEvents(ctx *Ctx, client *kubernetes.Clientset, eventPool *EventPool, nodePool *NodePool, hours int) {

	var nodeFound bool
	for {

		// Review stored events in the pool
		for _, event := range eventPool.Events.Items {

			// 0. Check if the pool has items: it changes dynamically
			if len(eventPool.Events.Items) == 0 {
				break
			}

			// 1. Check if node is still alive
			nodeFound = false

			for _, node := range nodePool.Nodes.Items {
				if event.InvolvedObject.Name == node.Name {
					nodeFound = true
				}
			}

			// TODO: check if following behaviour is needed on real production systems
			// 2. Check if the event is too old
			// Extract date from message
			eventMessage := strings.Fields(event.Message)
			rebalanceDate := eventMessage[len(eventMessage)-1]

			// Check the time window
			parsedDate, err := time.Parse(time.RFC3339, rebalanceDate)
			if err != nil {
				ctx.Logger.Info(ParseNotPossibleMessage)
			}

			difference := parsedDate.Sub(time.Now())

			// 3. Actual cleaning according to the previous conditions
			if math.Abs(difference.Hours()) > float64(hours) || !nodeFound {
				ctx.Logger.Infof(DeleteOldEventMessage, math.Abs(difference.Hours()), event.Namespace, event.Name)

				err = KubernetesDeleteEvent(client, event.Namespace, event.Name)
				if err != nil && !errors.IsNotFound(err) {
					ctx.Logger.Info(EventNotDeletedFromK8sMessage)
				}
			}
		}

		time.Sleep(WatchersLoopTime)
	}
}
