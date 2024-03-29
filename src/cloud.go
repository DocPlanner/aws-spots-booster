package main

import (
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	_ "golang.org/x/exp/slices"
	"k8s.io/utils/strings/slices"
	"strconv"
	"strings"
	"time"
)

const (
	// Constants related to the cloud provider
	AWSAutoscalingGroupsNodeGroupTag = "eks:nodegroup-name"

	// Constants related to processes
	ASGWatcherTriesBeforeFailing             = 2
	ASGWatcherSecondsBetweenTries            = 5
	ASGWatcherSecondsBetweenSynchronizations = 5
)

// WatchAutoScalingGroupsTags TODO
func WatchAutoScalingGroupsTags(ctx *Ctx, awsClient *session.Session, autoscalingGroupPool *AutoscalingGroupPool) {

	var autoscalingGroupNames []string

	for {
		// Try to get ASG names from memory several times
		for try := 0; try <= ASGWatcherTriesBeforeFailing; try++ {
			autoscalingGroupNames = GetAutoscalingGroupsNames(autoscalingGroupPool)

			if len(autoscalingGroupNames) > 0 {
				break
			}

			if try == ASGWatcherTriesBeforeFailing {
				ctx.Logger.Fatal("impossible to get ASGs tags from cloud. ASGs names are not loaded in memory")
			}
			ctx.Logger.Info("autoscaling groups are not parsed yet")
			time.Sleep(ASGWatcherSecondsBetweenTries * time.Second)
		}

		// Get ASGs tags from AWS
		tagsOutput, err := AwsDescribeAutoScalingGroupsTags(awsClient, autoscalingGroupNames)
		if err != nil {
			ctx.Logger.Info("say something") // TODO Improve logging
		}

		// Group tags by ASG name
		asgGroupedTags := map[string]map[string]string{}
		for _, tag := range tagsOutput.Tags {

			_, asgKeyFound := asgGroupedTags[*tag.ResourceId]
			if !asgKeyFound {
				asgGroupedTags[*tag.ResourceId] = map[string]string{}
			}

			asgGroupedTags[*tag.ResourceId][*tag.Key] = *tag.Value
		}

		// Store the tags into the actual ASGs object
		// Doing this way to block the pool the minimum time possible
		for _, asg := range autoscalingGroupPool.AutoscalingGroups {
			autoscalingGroupPool.Lock.Lock()
			asg.Tags = asgGroupedTags[asg.Name]
			autoscalingGroupPool.Lock.Unlock()
		}

		time.Sleep(ASGWatcherSecondsBetweenSynchronizations * time.Second)
	}
}

// AwsCreateSession TODO
func AwsCreateSession() (*session.Session, error) {

	awsSession, err := session.NewSessionWithOptions(session.Options{
		Config: aws.Config{
			CredentialsChainVerboseErrors: aws.Bool(true),
		},
		SharedConfigState: session.SharedConfigEnable,
	})

	// Specify profile for config and region for requests
	client := session.Must(awsSession, err)

	return client, err
}

// AwsDescribeAutoScalingGroupsTags TODO
func AwsDescribeAutoScalingGroupsTags(awsClient *session.Session, autoscalingGroupNames []string) (tagsOutput *autoscaling.DescribeTagsOutput, err error) {
	svc := autoscaling.New(awsClient)

	var parsedAutoscalingGroupNames []*string
	for _, autoscalingGroupName := range autoscalingGroupNames {
		parsedAutoscalingGroupNames = append(parsedAutoscalingGroupNames, aws.String(autoscalingGroupName))
	}

	input := &autoscaling.DescribeTagsInput{
		Filters: []*autoscaling.Filter{
			{
				Name:   aws.String("auto-scaling-group"),
				Values: parsedAutoscalingGroupNames,
			},
		},
	}

	tagsOutput, err = svc.DescribeTags(input)
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			case autoscaling.ErrCodeInvalidNextToken:
				fmt.Println(autoscaling.ErrCodeInvalidNextToken, aerr.Error())
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
		return tagsOutput, err
	}

	return tagsOutput, err
}

// AwsSetDesiredCapacity set the desired capacity for an Auto Scaling group
func AwsSetDesiredCapacity(awsClient *session.Session, asgName string, desiredCapacity int64) error {

	svc := autoscaling.New(awsClient)

	input := &autoscaling.SetDesiredCapacityInput{
		AutoScalingGroupName: aws.String(asgName),
		DesiredCapacity:      aws.Int64(desiredCapacity),
		HonorCooldown:        aws.Bool(false),
	}

	_, err := svc.SetDesiredCapacity(input)
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

	return err
}

// TODO
func AwsTerminateInstance(awsClient *session.Session, instanceName string) error {
	svc := autoscaling.New(awsClient)

	input := &autoscaling.TerminateInstanceInAutoScalingGroupInput{
		InstanceId:                     aws.String(instanceName),
		ShouldDecrementDesiredCapacity: aws.Bool(true),
	}

	_, err := svc.TerminateInstanceInAutoScalingGroup(input)
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

	return err
}

// CalculateDesiredCapacityASGs return a list of ASGs, the values for them are the number of instances needed
// This function will only return those ASGs that actually need changes according to the events
func CalculateDesiredCapacityASGs(autoscalingGroupPool *AutoscalingGroupPool, nodeGroupEventsCount map[string]int) (asgsDesiredCapacity map[string]int, err error) {

	asgsDesiredCapacity = map[string]int{}

	for _, asg := range autoscalingGroupPool.AutoscalingGroups {

		nodeGroupName := asg.Tags[AWSAutoscalingGroupsNodeGroupTag]
		if nodeGroupEventsCount[nodeGroupName] > 0 {
			currentCount, err := strconv.Atoi(asg.Health.Ready)
			if err != nil {
				break
			}

			// (Real Capacity) = (Ready Nodes) - (Rebalance Recommendations)
			// (Desired State) = (Real Capacity) + 2 * (Rebalance Recommendations)
			realCapacity := currentCount - nodeGroupEventsCount[nodeGroupName]
			asgsDesiredCapacity[asg.Name] = realCapacity + 2*nodeGroupEventsCount[nodeGroupName]
		}
	}

	return asgsDesiredCapacity, err
}

// SetDesiredCapacityASGs change DesiredCapacity field for a batch of ASGs in the cloud provider
// Arguments related to capacity are not pointers but explicit copies to avoid external modifications during changes
func SetDesiredCapacityASGs(ctx *Ctx, awsClient *session.Session, autoscalingGroupPool *AutoscalingGroupPool, asgsDesiredCapacity map[string]int) (err error) {

	// Get ignored node-groups from flags
	ignoredAsgs := strings.Split(*ctx.Flags.IgnoredAutoscalingGroups, ",")
	ignoredAsgs = slices.Filter(nil, ignoredAsgs, func(s string) bool { return s != "" })

	asgsMaxCapacity, err := GetAutoscalingGroupsMaxCapacity(autoscalingGroupPool)
	if err != nil {
		ctx.Logger.Infof("impossible to get max capacity for some asg: %v", err) // TODO ERROR
		return
	}

outterLoop:
	for asgName, asgDesiredCapacity := range asgsDesiredCapacity {

		// Skip ASG when must be ignored by flags configuration
		for _, ignoredASG := range ignoredAsgs {
			if asgName == ignoredASG {
				ctx.Logger.Infof("skipping changes for ignored asg: %s", asgName) // TODO INFO
				continue outterLoop
			}
		}

		asgDesiredCapacity = asgDesiredCapacity + *ctx.Flags.ExtraNodesOverCalculations
		ctx.Logger.Infof("setting desired capacity for '%s' to '%d'", asgName, asgDesiredCapacity) // TODO INFO

		// Check whether desired capacity is into the max capacity
		if asgDesiredCapacity > asgsMaxCapacity[asgName] {
			asgDesiredCapacity = asgsMaxCapacity[asgName]
			ctx.Logger.Infof("setting desired capacity for '%s' to the asg max '%d'", asgName, asgsMaxCapacity[asgName]) // TODO INFO
		}

		if *ctx.Flags.DryRun {
			continue
		}

		// Send the request to AWS
		err = AwsSetDesiredCapacity(
			awsClient,
			asgName,
			int64(asgDesiredCapacity))
		if err != nil {
			ctx.Logger.Infof("impossible to reflect changes on aws asg '%s': %v", asgName, err) // TODO ERROR
		}
	}

	return err
}
