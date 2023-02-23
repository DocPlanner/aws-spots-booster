package main

import (
	"context"
	"flag"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/homedir"
	"log"
	"net/http"
	"path/filepath"
	"time"
)

const (
	// SynchronizationScheduleSeconds The time in seconds between synchronizations
	SynchronizationScheduleSeconds = 2

	// Info messages
	GenerateRestClientMessage            = "generating rest client to connect to kubernetes"
	EventsOnPoolMessage                  = "events on the pool: %d"
	NodesOnPoolMessage                   = "nodes on the pool: %d"
	NodesByNodegroupMessage              = "nodes by nodegroup %v"
	EventsByNodegroupMessage             = "events by nodegroup %v"
	CordonedNodesByNodegroupMessage      = "cordoned nodes by nodegroup %v"
	RecentlyReadyNodesByNodegroupMessage = "recently ready nodes by nodegroup %v"
	ShowCalculationsMessage              = "show calculations for autocaling groups: %v"

	// Error messages
	GenerateAwsClientErrorMessage  = "error connecting to aws api: %s"
	GenerateRestClientErrorMessage = "error connecting to kubernetes api: %s"
	MetricsUpdateErrorMessage      = "imposible to update prometheus metrics"
	MetricsWebserverErrorMessage   = "imposible to launch metrics webserver: %s"
)

// SynchronizeBoosts execute all the processes needed to work. It is like main() but more related to the process
// This function is expected to be run as a goroutine
func SynchronizeBoosts(ctx *Ctx, client *kubernetes.Clientset) {

	// Update the nodes pool
	nodePool := &NodePool{}
	go WatchNodes(ctx, client, nodePool)

	// Update the events pool
	eventPool := &EventPool{}
	go WatchEvents(ctx, client, RebalanceEvent, eventPool)

	// Keep Kubernetes clean
	go CleanKubernetesEvents(ctx, client, eventPool, nodePool, 24)

	// Load Cluster Autoscaler status configmap on memory JIT
	autoscalingGroupPool := &AutoscalingGroupPool{}
	go WatchStatusConfigmap(ctx, client, autoscalingGroupPool)

	//
	awsClient, err := AwsCreateSession()
	if err != nil {
		ctx.Logger.Infof(GenerateAwsClientErrorMessage, err)
	}
	go WatchAutoScalingGroupsTags(ctx, awsClient, autoscalingGroupPool)

	// Launch a drainer in the background
	if !*ctx.Flags.DisableDrain {
		go DrainNodesUnderRisk(ctx, client, awsClient, eventPool, nodePool)
	}

	// Start working with the events
	for {

		ctx.Logger.Infof(EventsOnPoolMessage, len(eventPool.Events.Items))
		ctx.Logger.Infof(NodesOnPoolMessage, len(nodePool.Nodes.Items))

		// Get a map of node-group, each value is the count of its nodes
		nodeGroupNodesCount := GetNodeCountByNodeGroup(nodePool)
		ctx.Logger.Infof(NodesByNodegroupMessage, nodeGroupNodesCount)

		// Get a map of node-group, each value is the count of its events
		nodeGroupEventsCount := GetEventCountByNodeGroup(eventPool, nodePool)
		ctx.Logger.Infof(EventsByNodegroupMessage, nodeGroupEventsCount)

		// Get a map of node-group, each value is the count of its cordoned nodes
		nodeGroupCordonedNodesCount := GetCordonedNodeCountByNodeGroup(nodePool)
		ctx.Logger.Infof(CordonedNodesByNodegroupMessage, nodeGroupCordonedNodesCount)

		nodeGroupRecentReadyNodesCount := GetRecentlyReadyNodeCountByNodeGroup(nodePool, DurationToConsiderNewNodes, true)
		ctx.Logger.Infof(RecentlyReadyNodesByNodegroupMessage, nodeGroupRecentReadyNodesCount)

		// Calculate final capacity for the ASGs
		asgsDesiredCapacities, err := CalculateDesiredCapacityASGs(autoscalingGroupPool, nodeGroupEventsCount)
		if err != nil {
			ctx.Logger.Fatal(err)
		}
		ctx.Logger.Infof(ShowCalculationsMessage, asgsDesiredCapacities)

		err = SetDesiredCapacityASGs(ctx, awsClient, autoscalingGroupPool, asgsDesiredCapacities)
		if err != nil {
			ctx.Logger.Fatal(err)
		}

		// Update Prometheus metrics from AutoscalingGroups type data
		err = upgradePrometheusMetrics(eventPool, nodePool, autoscalingGroupPool)
		if err != nil {
			ctx.Logger.Info(MetricsUpdateErrorMessage)
		}

		time.Sleep(SynchronizationScheduleSeconds * time.Second)
	}
}

func main() {

	flags := &ControllerFlags{}

	// Get the values from flags
	flags.ConnectionMode = flag.String("connection-mode", "kubectl", "(optional) what type of connection to use: incluster, kubectl")
	flags.Kubeconfig = flag.String("kubeconfig", filepath.Join(homedir.HomeDir(), ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	flags.DryRun = flag.Bool("dry-run", false, "skip actual changes")

	flags.CAStatusNamespace = flag.String("ca-status-namespace", "kube-system", "kubernetes Namespace where to read cluster-utoscaler's status configmap")
	flags.CAConfigmapName = flag.String("ca-status-name", "cluster-autoscaler-status", "name of the cluster-autoscaler's status configmap")

	flags.IgnoredAutoscalingGroups = flag.String("ignored-autoscaling-groups", "", "comma-separated list of autoscaling-group names to ignore on ASGs boosting")
	flags.ExtraNodesOverCalculations = flag.Int("extra-nodes-over-calculation", 0, "extra nodes to add over calculated ones")

	flags.DisableDrain = flag.Bool("disable-drain", false, "disable drain-and-destroy process for nodes under risk (not recommended)")
	flags.TimeBetweenDrains = flag.Duration("time-between-drains", 15*time.Second, "duration between scheduling a batch drainages and the following (when new nodes are ready)")
	flags.DrainTimeout = flag.Duration("drain-timeout", 120*time.Second, "duration to consider a drain as done when not finished")
	flags.MaxConcurrentDrains = flag.Int("max-concurrent-drains", 5, "maximum number of nodes to drain at once")

	flags.MetricsPort = flag.String("metrics-port", "2112", "port where metrics web-server will run")
	flags.MetricsHost = flag.String("metrics-host", "0.0.0.0", "host where metrics web-server will run")
	flag.Parse()

	//
	mainCtx := context.Background()

	// Initialize the logger
	loggerConfig := zap.NewProductionConfig()
	loggerConfig.EncoderConfig.TimeKey = "timestamp"
	loggerConfig.EncoderConfig.EncodeTime = zapcore.TimeEncoderOfLayout(time.RFC3339)
	loggerConfig.Level.SetLevel(zap.InfoLevel)

	// Configure the logger
	logger, err := loggerConfig.Build()
	if err != nil {
		log.Fatal(err)
	}
	sugar := logger.Sugar()

	// As we are not using Kubebuilder yet, we need a main context
	// to be propagated into the functions that needs to log in a sugared way
	ctx := Ctx{
		Ctx:    mainCtx,
		Logger: sugar,
		Flags:  flags,
	}

	// Generate the Kubernetes client to modify the resources
	ctx.Logger.Info(GenerateRestClientMessage)
	client, err := GetKubernetesClient(*ctx.Flags.ConnectionMode, *ctx.Flags.Kubeconfig)
	if err != nil {
		ctx.Logger.Infof(GenerateRestClientErrorMessage, err)
	}

	// Parse Cluster Autoscaler's status configmap in the background
	go SynchronizeBoosts(&ctx, client)

	// Start a webserver for exposing metrics endpoint
	metricsHost := *ctx.Flags.MetricsHost + ":" + *ctx.Flags.MetricsPort
	http.Handle("/metrics", promhttp.Handler())
	err = http.ListenAndServe(metricsHost, nil)
	if err != nil {
		ctx.Logger.Infof(MetricsWebserverErrorMessage, err)
	}
}
