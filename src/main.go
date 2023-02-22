package main

import (
	"flag"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	_ "k8s.io/apimachinery/pkg/labels"
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
	GenerateRestClientMessage = "generating rest client to connect to kubernetes"

	// Error messages
	GenerateAwsClientErrorMessage  = "error connecting to aws api: %s"
	GenerateRestClientErrorMessage = "error connecting to kubernetes api: %s"
	MetricsUpdateErrorMessage      = "imposible to update prometheus metrics"
	MetricsWebserverErrorMessage   = "imposible to launch metrics webserver: %s"
)

// SynchronizeBoosts execute all the processes needed to work. It is like main() but more related to the process
// This function is expected to be run as a goroutine
func SynchronizeBoosts(client *kubernetes.Clientset, flags *ControllerFlags) {

	// Update the nodes pool
	nodePool := &NodePool{}
	go WatchNodes(client, nodePool)

	// Update the events pool
	eventPool := &EventPool{}
	go WatchEvents(client, RebalanceEvent, eventPool)

	// Keep Kubernetes clean
	go CleanKubernetesEvents(client, eventPool, nodePool, 24)

	// Load Cluster Autoscaler status configmap on memory JIT
	autoscalingGroupPool := &AutoscalingGroupPool{}
	go WatchStatusConfigmap(client, flags, autoscalingGroupPool)

	//
	awsClient, err := AwsCreateSession()
	if err != nil {
		log.Printf(GenerateAwsClientErrorMessage, err)
	}
	go WatchAutoScalingGroupsTags(awsClient, flags, autoscalingGroupPool)

	// Launch a drainer in the background
	if !*flags.DisableDrain {
		go DrainNodesUnderRisk(client, awsClient, flags, eventPool, nodePool)
	}

	// Start working with the events
	for {

		log.Printf("events on the pool: %d", len(eventPool.Events.Items))
		log.Printf("nodes on the pool: %d", len(nodePool.Nodes.Items))

		// Get a map of node-group, each value is the count of its nodes
		nodeGroupNodesCount := GetNodeCountByNodeGroup(nodePool)
		log.Printf("nodes by nodegroup %v", nodeGroupNodesCount)

		// Get a map of node-group, each value is the count of its events
		nodeGroupEventsCount := GetEventCountByNodeGroup(eventPool, nodePool)
		log.Printf("events by nodegroup %v", nodeGroupEventsCount)

		// Get a map of node-group, each value is the count of its cordoned nodes
		nodeGroupCordonedNodesCount := GetCordonedNodeCountByNodeGroup(nodePool)
		log.Printf("cordoned nodes by nodegroup %v", nodeGroupCordonedNodesCount)

		nodeGroupRecentReadyNodesCount := GetRecentlyReadyNodeCountByNodeGroup(nodePool, DurationToConsiderNewNodes, true)
		log.Printf("RECENTLY READY nodes by nodegroup %v", nodeGroupRecentReadyNodesCount)

		// Calculate final capacity for the ASGs
		asgsDesiredCapacities, err := CalculateDesiredCapacityASGs(autoscalingGroupPool, nodeGroupEventsCount)
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("show calculations for autocaling groups: %v", asgsDesiredCapacities)

		err = SetDesiredCapacityASGs(awsClient, flags, autoscalingGroupPool, asgsDesiredCapacities)
		if err != nil {
			log.Fatal(err)
		}

		// Update Prometheus metrics from AutoscalingGroups type data
		err = upgradePrometheusMetrics(eventPool, nodePool, autoscalingGroupPool)
		if err != nil {
			log.Print(MetricsUpdateErrorMessage)
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

	// Generate the Kubernetes client to modify the resources
	log.Printf(GenerateRestClientMessage)
	client, err := GetKubernetesClient(*flags.ConnectionMode, *flags.Kubeconfig)
	if err != nil {
		log.Printf(GenerateRestClientErrorMessage, err)
	}

	// Parse Cluster Autoscaler's status configmap in the background
	go SynchronizeBoosts(client, flags)

	// Start a webserver for exposing metrics endpoint
	metricsHost := *flags.MetricsHost + ":" + *flags.MetricsPort
	http.Handle("/metrics", promhttp.Handler())
	err = http.ListenAndServe(metricsHost, nil)
	if err != nil {
		log.Printf(MetricsWebserverErrorMessage, err)
	}
}
