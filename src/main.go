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
	"sync"
	"time"
)

const (
	// SynchronizationScheduleSeconds The time in seconds between synchronizations
	SynchronizationScheduleSeconds = 2

	// Info messages
	GenerateRestClientMessage = "generating rest client to connect to kubernetes"

	// Error messages
	GenerateRestClientErrorMessage = "error connecting to kubernetes api: %s"
	ConfigmapRetrieveErrorMessage  = "error obtaining cluster-autoscaler status configmap from the cluster"
	ConfigMapParseErrorMessage     = "error parsing status configmap (hint: syntax has changed between cluster-autoscaler versions?)"
	MetricsUpdateErrorMessage      = "imposible to update prometheus metrics"
	MetricsWebserverErrorMessage   = "imposible to launch metrics webserver: %s"
)

// SynchronizeStatus TODO
// Prometheus ref: https://prometheus.io/docs/guides/go-application/
func SynchronizeStatus(client *kubernetes.Clientset, flags *ControllerFlags) {

	var waitGroup sync.WaitGroup

	// Update the nodes pool
	nodePool := &NodePool{}
	//waitGroup.Add(1)
	go WatchNodes(client, nodePool)

	// Update the events pool
	eventPool := &EventPool{}
	//waitGroup.Add(1)
	go WatchEvents(client, RebalanceEvent, eventPool)

	// Keep Kubernetes clean
	//waitGroup.Add(1)
	go CleanKubernetesEvents(client, eventPool, nodePool, 24)

	// Load Cluster Autoscaler status configmap on memory JIT
	autoscalingGroupPool := &AutoscalingGroupPool{}
	//waitGroup.Add(1)
	go WatchStatusConfigmap(client, flags, autoscalingGroupPool)

	//
	awsClient, err := AwsCreateSession()
	if err != nil {
		log.Print("error creating AWS client")
	}
	//waitGroup.Add(1)
	go WatchAutoScalingGroupsTags(awsClient, flags, autoscalingGroupPool)

	// TODO: Put a wait group for all the watchers here before starting

	// Wait until all the watchers are warmed enough
	waitGroup.Wait()

	// Start working with the events
	for {
		log.Print("starting a synchronization loop")

		//log.Print(eventPool.Items)
		log.Printf("Events on the pool: %d", len(eventPool.Events.Items))
		log.Printf("Nodes on the pool: %d", len(nodePool.Nodes.Items))

		//log.Printf("Show cm in memory: %v", autoscalingGroupPool.AutoscalingGroups)
		for _, asg := range autoscalingGroupPool.AutoscalingGroups {
			log.Printf("Show asg in memory: %v, %v, %v", asg.Name, asg.Tags, asg.Health)
		}

		// Get a map of node-group, each value is the count of its events
		nodeGroupEventsCount := GetEventCountByNodeGroup(eventPool, nodePool)
		log.Printf("count by nodegroup %v", nodeGroupEventsCount)

		// Calculate final capacity for the ASGs
		asgsDesiredCapacities, err := CalculateDesiredCapacityASGs(autoscalingGroupPool.AutoscalingGroups, nodeGroupEventsCount)
		if err != nil {
			log.Fatal(err)
		}

		log.Printf("show calculations for autocaling groups: %v", asgsDesiredCapacities)

		err = SetDesiredCapacityASGs(awsClient, flags, asgsDesiredCapacities)

		// Update Prometheus metrics from AutoscalingGroups type data
		//err = upgradePrometheusMetrics(autoscalingGroupPool.AutoscalingGroups)
		//if err != nil {
		//	log.Print(MetricsUpdateErrorMessage)
		//}

		time.Sleep(SynchronizationScheduleSeconds * time.Second)
	}
}

func main() {

	flags := &ControllerFlags{}

	// Get the values from flags
	flags.ConnectionMode = flag.String("connection-mode", "kubectl", "(optional) What type of connection to use: incluster, kubectl")
	flags.Kubeconfig = flag.String("kubeconfig", filepath.Join(homedir.HomeDir(), ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	flags.CAStatusNamespace = flag.String("ca-status-namespace", "kube-system", "Kubernetes Namespace where to read cluster-utoscaler's status configmap")
	flags.CAConfigmapName = flag.String("ca-status-name", "cluster-autoscaler-status", "Name of the cluster-autoscaler's status configmap")
	flags.IgnoredNodegroups = flag.String("ignored-nodegroups", "", "Comma-separated list of node-group names to ignore on ASGs boosting")

	flags.MetricsPort = flag.String("metrics-port", "2112", "Port where metrics web-server will run")
	flags.MetricsHost = flag.String("metrics-host", "0.0.0.0", "Host where metrics web-server will run")
	flag.Parse()

	// Generate the Kubernetes client to modify the resources
	log.Printf(GenerateRestClientMessage)
	client, err := GetKubernetesClient(*flags.ConnectionMode, *flags.Kubeconfig)
	if err != nil {
		log.Printf(GenerateRestClientErrorMessage, err)
	}

	// Parse Cluster Autoscaler's status configmap in the background
	go SynchronizeStatus(client, flags)

	// Start a webserver for exposing metrics endpoint
	metricsHost := *flags.MetricsHost + ":" + *flags.MetricsPort
	http.Handle("/metrics", promhttp.Handler())
	err = http.ListenAndServe(metricsHost, nil)
	if err != nil {
		log.Printf(MetricsWebserverErrorMessage, err)
	}
}
