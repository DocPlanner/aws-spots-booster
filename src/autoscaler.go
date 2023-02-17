package main

import (
	"context"
	"encoding/json"
	"fmt"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"log"
	"regexp"
	"strings"
)

// WatchStatusConfigmap watches for changes on Cluster Autoscaler's status-configmap on k8s
// Done this way to reduce the calls done to Kube API
// This function must be executed as a go routine
func WatchStatusConfigmap(client *kubernetes.Clientset, flags *ControllerFlags, autoscalingGroupPool *AutoscalingGroupPool) {

	// Get configmap from the cluster
	configmapWatcher, err := client.CoreV1().ConfigMaps(*flags.CAStatusNamespace).Watch(context.TODO(), metav1.ListOptions{
		FieldSelector: fields.Set{"metadata.name": *flags.CAConfigmapName}.AsSelector().String(),
	})
	if err != nil {
		log.Fatal(ConfigmapRetrieveErrorMessage)
	}

	for event := range configmapWatcher.ResultChan() {

		configmapObject := event.Object.(*v1.ConfigMap)

		switch event.Type {
		case watch.Added, watch.Modified:
			log.Printf("configmap added: %s/%s", configmapObject.Namespace, configmapObject.Name) // TODO: Improve logging
			autoscalingGroupsNames := GetAutoscalingGroupsNames(configmapObject.Data["status"])
			autoscalingGroupsHealthArgs := GetAutoscalingGroupsHealthArguments(configmapObject.Data["status"])

			autoscalingGroups, err := GetAutoscalingGroupsObject(autoscalingGroupsNames, autoscalingGroupsHealthArgs)
			if err != nil {
				log.Print(ConfigMapParseErrorMessage)
			}

			autoscalingGroupPool.Lock.Lock()
			autoscalingGroupPool.AutoscalingGroups = *autoscalingGroups
			autoscalingGroupPool.Lock.Unlock()

		case watch.Deleted:
			log.Fatal("configmap deleted, stopping the program ") // TODO: Improve logging
		}
	}
}

// GetAutoscalingGroupsNames return an array with the names of the node-groups in the same order they are in the status
func GetAutoscalingGroupsNames(status string) []string {
	var autoscalingGroupNames []string
	nameRe := regexp.MustCompile(`(Name:\s*)([a-zA-Z0-9_-]+)`)
	autoscalingGroupMatches := nameRe.FindAllStringSubmatch(status, -1)

	for _, match := range autoscalingGroupMatches {
		autoscalingGroupNames = append(autoscalingGroupNames, match[2])
	}

	return autoscalingGroupNames
}

// GetAutoscalingGroupsHealthArguments return an array where each element is a string with all the arguments of one nodegroup
func GetAutoscalingGroupsHealthArguments(status string) []string {
	// Look for the node group health arguments
	nameRe := regexp.MustCompile(`(Health:\s*)([a-zA-Z0-9]+)\s*\((?P<args>(.*)+)\)`)
	autoscalingGroupMatches := nameRe.FindAllStringSubmatch(status, -1)

	// Filter arguments string
	var healthArgs []string
	symbolsRe := regexp.MustCompile(`[^\w=]`)

	for _, match := range autoscalingGroupMatches {
		// Delete all the symbols from the match
		match[3] = symbolsRe.ReplaceAllString(match[3], " ")

		// Separate string into an array of arguments
		if !strings.Contains(match[3], "minSize") || !strings.Contains(match[3], "maxSize") {
			continue
		}

		healthArgs = append(healthArgs, match[3])
	}

	return healthArgs
}

// GetAutoscalingGroupsObject return a AutoscalingGroups type with all the data from status ConfigMap already parsed
func GetAutoscalingGroupsObject(autoscalingGroupsNames []string, autoscalingGroupsHealthStatus []string) (*AutoscalingGroups, error) {
	var autoscalingGroup AutoscalingGroup
	var autoscalingGroups AutoscalingGroups

	stringRe := regexp.MustCompile(`(\w+)=([0-9]+)`)
	replacePattern := `"$1":"$2",`

	for i, name := range autoscalingGroupsNames {
		// Include NG name
		autoscalingGroup.Name = name

		// Parse NG args
		arguments := stringRe.ReplaceAllString(autoscalingGroupsHealthStatus[i], replacePattern)
		arguments = strings.TrimSpace(arguments)       // Fix extra spaces
		arguments = strings.TrimSuffix(arguments, ",") // Fix trailing colon
		arguments = fmt.Sprintf("{%s}", arguments)     // Complete json syntax

		// Parse string args into NG Object
		err := json.Unmarshal([]byte(arguments), &autoscalingGroup.Health)
		if err != nil {
			return &autoscalingGroups, err
		}

		// Append NG to the NG pool
		autoscalingGroups = append(autoscalingGroups, autoscalingGroup)
	}

	return &autoscalingGroups, nil
}