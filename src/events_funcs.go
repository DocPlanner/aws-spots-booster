package main

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/utils/strings/slices"
	"sort"
	"time"
)

const (

	// AWSNodeGroupLabel is the node's label to store the name of the node-group for a node
	AWSNodeGroupLabel = "eks.amazonaws.com/nodegroup"

	// IgnoreRecentReadyNodeAnnotation is an annotation to ignore recently added nodes from recently added lists
	IgnoreRecentReadyNodeAnnotation      = "asbooster.docplanner.com/ignore-recent-ready"
	IgnoreRecentReadyNodeAnnotationValue = "true"
)

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
			//ctx.Logger.Info(node)
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

// GetNodesByNodeGroup return a list of Node-groups, the value for each of them is a list with its nodes
func GetNodesByNodeGroup(nodePool *NodePool) (nodeGroupNodeList map[string][]*v1.Node) {

	nodeGroupNodeList = map[string][]*v1.Node{}

	// Fill the slice with defaults, just in case no nodes for the node-groups
	nodeGroupNames := GetNodeGroupNames(nodePool)
	for _, nodeGroupName := range nodeGroupNames {
		nodeGroupNodeList[nodeGroupName] = []*v1.Node{}
	}

	for _, node := range nodePool.Nodes.Items {

		// Check if nodegroup label is present
		nodeGroupName, nodeGroupLabelFound := node.Labels[AWSNodeGroupLabel]
		if !nodeGroupLabelFound {
			continue
		}

		// Nodegroup name found, increase the account there for this event
		nodeGroupNodeList[nodeGroupName] = append(nodeGroupNodeList[nodeGroupName], &node)
	}

	return nodeGroupNodeList
}

// GetNodeCountByNodeGroup return a list of Node-groups, the value for each of them is its number of nodes
func GetNodeCountByNodeGroup(nodePool *NodePool) (nodeGroupNodesCount map[string]int) {

	nodeGroupNodeLists := GetNodesByNodeGroup(nodePool)

	nodeGroupNodesCount = map[string]int{}

	// Count events related to each node group
	for nodeGroupName, nodeGroupNodeList := range nodeGroupNodeLists {
		nodeGroupNodesCount[nodeGroupName] = len(nodeGroupNodeList)
	}

	return nodeGroupNodesCount
}

// GetCordonedNodesByNodeGroup return a list of Node-groups, the value for each of them is a list with its cordoned nodes
func GetCordonedNodesByNodeGroup(nodePool *NodePool) (nodeGroupNodeList map[string][]*v1.Node) {

	nodeGroupNodeList = map[string][]*v1.Node{}

	// Fill the slice with defaults, just in case no nodes for the node-groups
	nodeGroupNames := GetNodeGroupNames(nodePool)
	for _, nodeGroupName := range nodeGroupNames {
		nodeGroupNodeList[nodeGroupName] = []*v1.Node{}
	}

	for _, node := range nodePool.Nodes.Items {

		// Check if nodegroup label is present
		nodeGroupName, nodeGroupLabelFound := node.Labels[AWSNodeGroupLabel]
		if !nodeGroupLabelFound {
			continue
		}

		//
		if node.Spec.Unschedulable == true {
			nodeGroupNodeList[nodeGroupName] = append(nodeGroupNodeList[nodeGroupName], &node)
		}
	}

	return nodeGroupNodeList
}

// GetCordonedNodeCountByNodeGroup return a list of Node-groups, the value for each of them is its number of cordoned nodes
func GetCordonedNodeCountByNodeGroup(nodePool *NodePool) (nodeGroupNodesCount map[string]int) {

	nodeGroupNodeLists := GetCordonedNodesByNodeGroup(nodePool)

	nodeGroupNodesCount = map[string]int{}

	// Count events related to each node group
	for nodeGroupName, nodeGroupNodeList := range nodeGroupNodeLists {
		nodeGroupNodesCount[nodeGroupName] = len(nodeGroupNodeList)
	}

	return nodeGroupNodesCount
}

// GetRecentlyReadyNodesByNodeGroup return a list of Node-groups, the value for each of them is its latest Ready nodes.
// Nodes annotated with IgnoreRecentReadyNodeAnnotation can be ignored
// They are returned ordered by creation timestamp
func GetRecentlyReadyNodesByNodeGroup(nodePool *NodePool, durationBefore time.Duration, ignoreAnnotated bool) (nodeGroupNodeList map[string][]*v1.Node) {

	nodeGroupNodeList = map[string][]*v1.Node{}

	// Fill the slice with defaults, just in case no nodes for the node-groups
	nodeGroupNames := GetNodeGroupNames(nodePool)
	for _, nodeGroupName := range nodeGroupNames {
		nodeGroupNodeList[nodeGroupName] = []*v1.Node{}
	}

	// Look for recently Ready nodes
outterLoop:
	for _, node := range nodePool.Nodes.Items {

		// Check if nodegroup label is present
		nodeGroupName, nodeGroupLabelFound := node.Labels[AWSNodeGroupLabel]
		if !nodeGroupLabelFound {
			continue
		}

		// Ignore nodes with well-known annotation
		if ignoreAnnotated {
			for annotationKey, _ := range node.Annotations {
				if annotationKey == IgnoreRecentReadyNodeAnnotation {
					continue outterLoop
				}
			}
		}

		// Look into the conditions for last Ready transition
		for _, condition := range node.Status.Conditions {
			currentTime := time.Now()

			if condition.Type == v1.NodeReady && node.Spec.Unschedulable == false {
				someTimeBefore := currentTime.Add(durationBefore)
				if condition.LastTransitionTime.After(someTimeBefore) {
					nodeGroupNodeList[nodeGroupName] = append(nodeGroupNodeList[nodeGroupName], node.DeepCopy())
				}
			}
		}
	}

	return nodeGroupNodeList
}

// GetRecentlyReadyNodeCountByNodeGroup return a list of Node-groups, the value for each of them is its number of the latest Ready nodes.
// Nodes annotated with IgnoreRecentReadyNodeAnnotation can be ignored
func GetRecentlyReadyNodeCountByNodeGroup(nodePool *NodePool, durationBefore time.Duration, ignoreAnnotated bool) (nodeGroupNodesCount map[string]int) {

	nodeGroupNodeLists := GetRecentlyReadyNodesByNodeGroup(nodePool, durationBefore, ignoreAnnotated)

	nodeGroupNodesCount = map[string]int{}

	// Count events related to each node group
	for nodeGroupName, nodeGroupNodeList := range nodeGroupNodeLists {
		nodeGroupNodesCount[nodeGroupName] = len(nodeGroupNodeList)
	}

	return nodeGroupNodesCount
}

// GetSortedNodeList return a sorted copy of a node's list. Sorted by creation timestamp
func GetSortedNodeList(nodeList []*v1.Node, descending bool) []*v1.Node {

	var nodeListCopy []*v1.Node
	nodeListCopy = nodeList

	if descending {
		sort.Slice(nodeListCopy, func(i, j int) bool {
			return nodeListCopy[j].CreationTimestamp.Before(&nodeListCopy[i].CreationTimestamp)
		})
		return nodeListCopy
	}

	sort.Slice(nodeListCopy, func(i, j int) bool {
		return nodeListCopy[i].CreationTimestamp.Before(&nodeListCopy[j].CreationTimestamp)
	})

	return nodeListCopy
}
