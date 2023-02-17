package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

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
