package main

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (

	// MetricsPrefix
	MetricsPrefix = "aws_spots_booster_"
)

// TODO UPDATE METRICS FOR THIS CONTROLLER
var (
	mNodegroupEventsTotal = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: MetricsPrefix + "events_total",
		Help: "number of rebalance recommendation events per nodegroup",
	}, []string{"nodegroup"})

	mNodegroupNodesTotal = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: MetricsPrefix + "nodes_total",
		Help: "number of nodes per nodegroup",
	}, []string{"nodegroup"})

	mNodegroupCordonedNodesTotal = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: MetricsPrefix + "cordoned_nodes_total",
		Help: "number of cordoned nodes per nodegroup",
	}, []string{"nodegroup"})

	mNodegroupRecentlyReadyNodesTotal = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: MetricsPrefix + "recently_ready_nodes_total",
		Help: "number of recently ready nodes per nodegroup. those created since " + DurationToConsiderNewNodes.String() + " ago",
	}, []string{"nodegroup"})
)

// TODO
func upgradePrometheusMetrics(eventPool *EventPool, nodePool *NodePool, autoscalingGroupPool *AutoscalingGroupPool) (err error) {

	nodegroups := GetNodeGroupNames(nodePool)

	// Get a map of node-groups, each value is the count of its events
	nodeGroupEventsCount := GetEventCountByNodeGroup(eventPool, nodePool)

	// Get a map of node-groups, each value is the count of its nodes
	nodeGroupNodesCount := GetNodeCountByNodeGroup(nodePool)

	// Get a map of node-groups, each value is the count of its cordoned nodes
	nodeGroupCordonedNodesCount := GetCordonedNodeCountByNodeGroup(nodePool)

	// Get a map of node-group, each value is the count of its recently-ready nodes
	nodeGroupRecentReadyNodesCount := GetRecentlyReadyNodeCountByNodeGroup(nodePool, DurationToConsiderNewNodes, true) // TODO: decide the policy

	for _, nodegroupName := range nodegroups {

		// Convert all the values to proper format
		nodegroupEventsTotal := float64(nodeGroupEventsCount[nodegroupName])
		nodegroupNodesTotal := float64(nodeGroupNodesCount[nodegroupName])
		nodegroupCordonedNodesTotal := float64(nodeGroupCordonedNodesCount[nodegroupName])
		nodegroupRecentlyReadyNodesTotal := float64(nodeGroupRecentReadyNodesCount[nodegroupName])

		// Update all the metrics for this nodegroup
		mNodegroupEventsTotal.WithLabelValues(nodegroupName).Set(nodegroupEventsTotal)
		mNodegroupNodesTotal.WithLabelValues(nodegroupName).Set(nodegroupNodesTotal)
		mNodegroupCordonedNodesTotal.WithLabelValues(nodegroupName).Set(nodegroupCordonedNodesTotal)
		mNodegroupRecentlyReadyNodesTotal.WithLabelValues(nodegroupName).Set(nodegroupRecentlyReadyNodesTotal)
	}

	return nil
}
