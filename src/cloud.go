package main

import (
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"strconv"
	"strings"
)

// CalculateDesiredCapacityASG return a list of ASGs, the values for them are the number of instances needed
func CalculateDesiredCapacityASG(autoscalingGroups AutoscalingGroups, nodeGroupEventsCount map[string]int) (asgsDesiredCapacity map[string]int, err error) {

	asgsDesiredCapacity = map[string]int{}

	// Iterate over affected node-groups
	for nodeGroupName, eventCount := range nodeGroupEventsCount {

		// Iterate over ASGS to look for the name matching the node-group
	innerList:
		for _, asg := range autoscalingGroups {
			if !strings.Contains(asg.Name, nodeGroupName) {
				continue innerList
			}

			// Nodegroup name found on ASG, start the calculation
			currentCount, err := strconv.Atoi(asg.Health.Ready)
			if err != nil {
				break
			}
			asgsDesiredCapacity[asg.Name] = currentCount + eventCount
		}
	}

	return asgsDesiredCapacity, err
}

func CreateAwsSession() (*session.Session, error) {

	awsSession, err := session.NewSessionWithOptions(session.Options{
		Config: aws.Config{
			Region:                        aws.String("eu-central-1"),
			CredentialsChainVerboseErrors: aws.Bool(true),
		},
		Profile:           "account-p2",
		SharedConfigState: session.SharedConfigEnable,
	})

	// Specify profile for config and region for requests
	client := session.Must(awsSession, err)

	return client, err
}

// SetDesiredCapacity set the desired capacity for an Auto Scaling group
func SetDesiredCapacity(awsClient *session.Session, asgName string, desiredCapacity int64) error {

	svc := autoscaling.New(awsClient)

	input := &autoscaling.SetDesiredCapacityInput{
		AutoScalingGroupName: aws.String(asgName),
		DesiredCapacity:      aws.Int64(desiredCapacity),
		HonorCooldown:        aws.Bool(false),
	}

	result, err := svc.SetDesiredCapacity(input)
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			case autoscaling.ErrCodeScalingActivityInProgressFault:
				fmt.Println(autoscaling.ErrCodeScalingActivityInProgressFault, aerr.Error())
			case autoscaling.ErrCodeResourceContentionFault:
				fmt.Println(autoscaling.ErrCodeResourceContentionFault, aerr.Error())
			default:
				fmt.Println(aerr.Error())
			}
		} else {
			// Print the error, cast err to awserr.Error to get the Code and
			// Message from an error.
			fmt.Println(err.Error())
		}
		return err
	}

	fmt.Println(result)
	return err
}
