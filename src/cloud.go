package main

import (
    "fmt"
    "github.com/aws/aws-sdk-go/aws"
    "github.com/aws/aws-sdk-go/aws/awserr"
    "github.com/aws/aws-sdk-go/aws/session"
    "github.com/aws/aws-sdk-go/service/autoscaling"
    _ "golang.org/x/exp/slices"
    "k8s.io/utils/strings/slices"
    "log"
    "strconv"
    "strings"
)

// AwsCreateSession TODO
func AwsCreateSession() (*session.Session, error) {

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

// AwsSetDesiredCapacity set the desired capacity for an Auto Scaling group
func AwsSetDesiredCapacity(awsClient *session.Session, asgName string, desiredCapacity int64) error {

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

// CalculateDesiredCapacityASGs return a list of ASGs, the values for them are the number of instances needed
func CalculateDesiredCapacityASGs(autoscalingGroups AutoscalingGroups, nodeGroupEventsCount map[string]int) (asgsDesiredCapacity map[string]int, err error) {

    asgsDesiredCapacity = map[string]int{}

    // Iterate over affected node-groups
    for nodeGroupName, eventCount := range nodeGroupEventsCount {

        // Iterate over ASGS to look for the name matching the node-group
        // TODO use Levenshtein distance to find the node-group
        for _, asg := range autoscalingGroups {
            if !strings.Contains(asg.Name, nodeGroupName) {
                continue
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

// SetDesiredCapacityASGs TODO
// Arguments related to capacity are not pointers but explicit copies to avoid external modifications during changes
func SetDesiredCapacityASGs(awsClient *session.Session, flags *ControllerFlags, asgsDesiredCapacities map[string]int) (err error) {

    // TODO Should we ignore by ASG instead??

    // Get ignored node-groups from flags
    ignoredNodegroups := strings.Split(*flags.IgnoredNodegroups, ",")
    ignoredNodegroups = slices.Filter(nil, ignoredNodegroups, func(s string) bool { return s != "" })

outterLoop:
    for asgName, desiredCapacity := range asgsDesiredCapacities {

        // Skip ASG when related nodegroup must be ignored
        // TODO: Filter with more advanced technics
        for _, ignoredNodeGroup := range ignoredNodegroups {
            if strings.Contains(asgName, ignoredNodeGroup) {
                log.Printf("skipping asg changes: %s ignored ng '%v'", asgName, ignoredNodeGroup)
                continue outterLoop
            }
        }

        log.Printf("setting desired capacity for '%s' to '%d'", asgName, desiredCapacity)

        // Send the request to AWS
        //err = SetDesiredCapacity(
        //	awsClient,
        //	"eks-nodes-app-spot-0ec22d5a-1d53-aa90-9a86-46470ecca2af",
        //	23)
        //if err != nil {
        //	log.Fatal(err)
        //}
    }

    return err
}
