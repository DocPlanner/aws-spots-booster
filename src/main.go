package main

import (
	"context"
	"flag"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	GenerateRestClientErrorMessage = "error connecting to kubernetes api: %s"
	ConfigmapRetrieveErrorMessage  = "error obtaining cluster-autoscaler status configmap from the cluster"
	ConfigMapParseErrorMessage     = "error parsing status configmap (hint: syntax has changed between cluster-autoscaler versions?)"
	MetricsUpdateErrorMessage      = "imposible to update prometheus metrics"
	MetricsWebserverErrorMessage   = "imposible to launch metrics webserver: %s"
)

// SynchronizeStatus TODO
// Prometheus ref: https://prometheus.io/docs/guides/go-application/
func SynchronizeStatus(client *kubernetes.Clientset, namespace string, configmap string) {

	// Update the nodes pool
	nodePool := &NodePool{}
	go WatchNodes(client, nodePool)

	// Update the events pool
	eventPool := &EventPool{}
	go WatchEvents(client, RebalanceEvent, eventPool)

	// Keep Kubernetes clean
	go CleanKubernetesEvents(client, eventPool, nodePool, 24)

	//awsClient, err := CreateAwsSession()
	_, err := CreateAwsSession()
	if err != nil {
		log.Print("error creating AWS client")
	}

	// Start working with the events
	for {
		log.Print("starting a synchronization loop")

		// Get configmap from the cluster
		configMap, err := client.CoreV1().ConfigMaps(namespace).Get(context.TODO(), configmap, metav1.GetOptions{})
		if err != nil {
			log.Print(ConfigmapRetrieveErrorMessage)

			time.Sleep(SynchronizationScheduleSeconds * time.Second)
			continue
		}

		// Look for all the ASG names and health arguments
		autoscalingGroupsNames := GetAutoscalingGroupsNames(configMap.Data["status"])
		autoscalingGroupsHealthArgs := GetAutoscalingGroupsHealthArguments(configMap.Data["status"])

		// Retrieve data as a Go struct
		autoscalingGroups, err := GetAutoscalingGroupsObject(autoscalingGroupsNames, autoscalingGroupsHealthArgs)
		if err != nil {
			log.Print(ConfigMapParseErrorMessage)
		}

		//log.Print(eventPool.Items)
		log.Printf("Events on the pool: %d", len(eventPool.Events.Items))
		log.Printf("Nodes on the pool: %d", len(nodePool.Nodes.Items))

		// Get a map of node-group, each value is a list of its events
		//nodeGroupsEvents := GetEventsByNodeGroup(eventPool, nodeList)
		//log.Print("Showing NGs")
		//log.Print(nodeGroupsEvents)

		nodeGroupNames := GetNodeGroupNames(nodePool)
		log.Print(nodeGroupNames)

		// Get a map of node-group, each value is the count of its events
		nodeGroupEventsCount := GetEventCountByNodeGroup(eventPool, nodePool)
		//log.Print("Showing NG count")
		//log.Print(nodeGroupEventsCount)

		// Calculate final capacity for the ASGs
		asgsDesiredCapacities, err := CalculateDesiredCapacityASG(*autoscalingGroups, nodeGroupEventsCount)
		if err != nil {
			log.Fatal(err)
		}

		log.Print("Showing calculations")
		log.Print(asgsDesiredCapacities)

		for asgName, desiredCapacity := range asgsDesiredCapacities {

			log.Printf("setting capacity for '%s' to '%d'", asgName, desiredCapacity)

			// Send the request to AWS
			//err = SetDesiredCapacity(
			//	awsClient,
			//	"eks-nodes-app-spot-0ec22d5a-1d53-aa90-9a86-46470ecca2af",
			//	23)
			//if err != nil {
			//	log.Fatal(err)
			//}
		}

		// No changes, just reschedule calculations
		//if len(desiredCapacity) == 0 {
		//    continue
		//}

		//log.Print("Desired capacity for the ASGs are: ")
		//log.Print(desiredCapacity)

		// Update Prometheus metrics from AutoscalingGroups type data
		err = upgradePrometheusMetrics(autoscalingGroups)
		if err != nil {
			log.Print(MetricsUpdateErrorMessage)
		}

		time.Sleep(SynchronizationScheduleSeconds * time.Second)
	}
}

func main() {
	// Get the values from flags
	connectionMode := flag.String("connection-mode", "kubectl", "(optional) What type of connection to use: incluster, kubectl")
	kubeconfig := flag.String("kubeconfig", filepath.Join(homedir.HomeDir(), ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	namespaceFlag := flag.String("namespace", "kube-system", "Kubernetes Namespace where to synchronize the certificates")
	configmapFlag := flag.String("configmap", "cluster-autoscaler-status", "Name of the cluster-autoscaler's status configmap")

	metricsPortFlag := flag.String("metrics-port", "2112", "Port where metrics web-server will run")
	metricsHostFlag := flag.String("metrics-host", "0.0.0.0", "Host where metrics web-server will run")
	flag.Parse()

	// Generate the Kubernetes client to modify the resources
	log.Printf(GenerateRestClientMessage)
	client, err := GetKubernetesClient(*connectionMode, *kubeconfig)
	if err != nil {
		log.Printf(GenerateRestClientErrorMessage, err)
	}

	// Parse Cluster Autoscaler's status configmap in the background
	go SynchronizeStatus(client, *namespaceFlag, *configmapFlag)

	// Start a webserver for exposing metrics endpoint
	metricsHost := *metricsHostFlag + ":" + *metricsPortFlag
	http.Handle("/metrics", promhttp.Handler())
	err = http.ListenAndServe(metricsHost, nil)
	if err != nil {
		log.Printf(MetricsWebserverErrorMessage, err)
	}
}
