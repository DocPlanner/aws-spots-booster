package main

import (
	v1 "k8s.io/api/core/v1"
	"sync"
)

// HealthStatus represents the status of a node group
type HealthStatus struct {
	Ready            string `json:"ready"`   // Number of nodes ready to schedule pods
	Unready          string `json:"unready"` // Number of nodes not ready to schedule pods in it
	NotStarted       string `json:"notStarted"`
	LongNotStarted   string `json:"longNotStarted"`
	Registered       string `json:"registered"`
	LongUnregistered string `json:"longUnregistered"`

	CloudProviderTarget  string `json:"cloudProviderTarget"` // Desired number of nodes in the provider
	CloudProviderMinSize string `json:"maxSize"`             // Minimum number of nodes in the provider
	CloudProviderMaxSize string `json:"minSize"`             // Maximum number of nodes in the provider
}

// AutoscalingGroup represents available metrics for one autoscaling group
type AutoscalingGroup struct {
	Name   string
	Health HealthStatus
	Tags   map[string]string
}

// AutoscalingGroups represents a group of autoscaling groups
type AutoscalingGroups = []*AutoscalingGroup

// Pools represent lockable group of different types, that are accessed/modified by goroutines

// AutoscalingGroupPool represents a group of autoscaling groups
type AutoscalingGroupPool struct {
	Lock              sync.Mutex
	AutoscalingGroups AutoscalingGroups
}

// EventPool represents a list of events stored from Kubernetes to handle API server events' TTL
type EventPool struct {
	Lock   sync.Mutex
	Events v1.EventList
}

// NodePool represents a list of nodes stored from Kubernetes
type NodePool struct {
	Lock  sync.Mutex
	Nodes v1.NodeList
}

// Controller stuff

// ControllerFlags represents the group of flags needed by the controller
type ControllerFlags struct {
	ConnectionMode *string
	Kubeconfig     *string

	CAStatusNamespace *string
	CAConfigmapName   *string

	IgnoredNodegroups *string

	MetricsPort *string
	MetricsHost *string
}
