package main

import (
	"context"
	"fmt"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/strings/slices"
	"log"
	"math"
	"strings"
	"time"
)

const (

	// Event reasons
	RebalanceEvent = "RebalanceRecommendation"

	// Nodegroup labels
	AWSNodeGroupLabel = "eks.amazonaws.com/nodegroup"

	//
	EventsGCLoopTime = 2 * time.Second
)

// WatchNodes watches for nodes on k8s and keep a pool up-to-date with them
// Done this way to reduce the calls done to Kube API
// This function must be executed as a go routine
func WatchNodes(client *kubernetes.Clientset, nodePool *NodePool) {

	nodesWatcher, err := client.CoreV1().Nodes().Watch(context.TODO(), metav1.ListOptions{})

	if err != nil {
		log.Fatal(err)
	}

	for event := range nodesWatcher.ResultChan() {

		nodeObject := event.Object.(*v1.Node)

		switch event.Type {
		case watch.Added:
			log.Printf("node added, checking the node pool: %s", nodeObject.Name)

			nodePool.Lock.Lock()
			nodePool.Nodes.Items = append(nodePool.Nodes.Items, *nodeObject)
			nodePool.Lock.Unlock()

		case watch.Deleted:
			log.Printf("node deleted, checking the node pool: %s", nodeObject.Name)

			// Remove the event from the pool: last item to current position, then delete last
			for storedNodeIndex, storedNode := range nodePool.Nodes.Items {

				if nodeObject.Name == storedNode.Name {
					nodePool.Lock.Lock()
					nodePool.Nodes.Items[storedNodeIndex] = nodePool.Nodes.Items[len(nodePool.Nodes.Items)-1]
					nodePool.Nodes.Items = nodePool.Nodes.Items[:len(nodePool.Nodes.Items)-1]
					nodePool.Lock.Unlock()
					break
				}
			}
		}
	}
}

// WatchEvents watches for some reasoned events on k8s and keep a pool up-to-date with them
// This function must be executed as a go routine
func WatchEvents(client *kubernetes.Clientset, eventReason string, eventPool *EventPool) {

	eventWatcher, err := client.CoreV1().Events("default").Watch(context.TODO(), metav1.ListOptions{
		FieldSelector: fmt.Sprintf("reason=%s", eventReason),
	})

	if err != nil {
		log.Fatal(err)
	}

	for event := range eventWatcher.ResultChan() {

		eventObject := event.Object.(*v1.Event)

		switch event.Type {
		case watch.Added:
			log.Printf("event added, checking the pool: %s/%s", eventObject.Namespace, eventObject.Name)

			// Filter repeated events coming from same nodes. New will replace the old
			for storedEventIndex, storedEvent := range eventPool.Events.Items {
				if eventObject.InvolvedObject.Name == storedEvent.InvolvedObject.Name {
					eventPool.Lock.Lock()
					eventPool.Events.Items[storedEventIndex] = *eventObject
					eventPool.Lock.Unlock()
					break
				}
			}

			// Not found, store it
			eventPool.Events.Items = append(eventPool.Events.Items, *eventObject)

		case watch.Deleted:
			log.Printf("event deleted, checking the pool: %s/%s", eventObject.Namespace, eventObject.Name)

			// Remove the event from the pool: last item to current position, then delete last
			for storedEventIndex, storedEvent := range eventPool.Events.Items {

				if eventObject.InvolvedObject.Name == storedEvent.InvolvedObject.Name {
					eventPool.Lock.Lock()
					eventPool.Events.Items[storedEventIndex] = eventPool.Events.Items[len(eventPool.Events.Items)-1]
					eventPool.Events.Items = eventPool.Events.Items[:len(eventPool.Events.Items)-1]
					eventPool.Lock.Unlock()
					break
				}
			}
		}
	}
}

// CleanKubernetesEvents delete old/nosense events from Kubernetes
// This function must be executed as a go routine
func CleanKubernetesEvents(client *kubernetes.Clientset, eventPool *EventPool, nodePool *NodePool, hours int) {

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
				log.Print("impossible to parse date on the message")
			}

			difference := parsedDate.Sub(time.Now())

			log.Printf("difference %d", difference)
			log.Printf("node found %v", nodeFound)

			// 3. Actual cleaning according to the previous conditions
			if math.Abs(difference.Hours()) > float64(hours) || !nodeFound {
				log.Printf("An event is too old (%s), deleting: %s/%s",
					math.Abs(difference.Hours()), event.Namespace, event.Name)

				err = DeleteKubernetesEvent(client, event.Namespace, event.Name)
				if err != nil {
					log.Print("impossible to delete event from K8s")
				}
			}
		}

		time.Sleep(EventsGCLoopTime)
	}
}

// GetNodeGroupNames return a slice with the names of the node-groups
func GetNodeGroupNames(nodePool *NodePool) (nodeGroupNames []string) {

	for _, node := range nodePool.Nodes.Items {

		// Check if nodegroup label is present
		nodeGroupName, nodeGroupLabelFound := node.Labels[AWSNodeGroupLabel]
		if !nodeGroupLabelFound {
			continue
		}

		// Nodegroup name found, store it
		if !slices.Contains(nodeGroupNames, nodeGroupName) {
			nodeGroupNames = append(nodeGroupNames, nodeGroupName)
		}
	}
	return nodeGroupNames
}

// GetEventsByNodeGroup return a list of Node-groups, the value for each of them is a list with its events
func GetEventsByNodeGroup(eventPool *EventPool, nodePool *NodePool) (nodeGroupEventList map[string][]*v1.Event) {

	nodeGroupEventList = map[string][]*v1.Event{}

	// Fill the slice with defaults, just in case no events for the node-groups
	nodeGroupNames := GetNodeGroupNames(nodePool)
	for _, nodeGroupName := range nodeGroupNames {
		nodeGroupEventList[nodeGroupName] = []*v1.Event{}
	}

	for _, event := range eventPool.Events.Items {

		// Look for the node related to current event to get the nodegroup label
	innerLoop:
		for _, node := range nodePool.Nodes.Items {
			//log.Print(node)
			if node.Name != event.InvolvedObject.Name {
				continue innerLoop
			}

			// Check if nodegroup label is present
			nodeGroupName, nodeGroupLabelFound := node.Labels[AWSNodeGroupLabel]
			if !nodeGroupLabelFound {
				continue
			}

			// Nodegroup name found, increase the account there for this event
			nodeGroupEventList[nodeGroupName] = append(nodeGroupEventList[nodeGroupName], &event)
		}
	}

	return nodeGroupEventList
}

// GetEventCountByNodeGroup return a list of Node-groups, the value for each of them is its number of events
func GetEventCountByNodeGroup(eventPool *EventPool, nodePool *NodePool) (nodeGroupEventsCount map[string]int) {

	nodeGroupEventLists := GetEventsByNodeGroup(eventPool, nodePool)

	nodeGroupEventsCount = map[string]int{}

	// Count events related to each node group
	for nodeGroupName, nodeGroupEventList := range nodeGroupEventLists {
		nodeGroupEventsCount[nodeGroupName] = len(nodeGroupEventList)
	}

	return nodeGroupEventsCount
}
