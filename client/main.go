package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/arn"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ecs"
	"github.com/fatih/color"
)

var region string
var hubPatch int

func init() {
	flag.StringVar(&region, "region", "us-east-1", "AWS region, in which to create a jacktrip instance")
	flag.IntVar(&hubPatch, "hubpatch", 2, "Hub auto audio patch, only has effect if running HUB SERVER mode, 0=server-to-clients, 1=client loopback, 2=clients can hear all clients except themselves, 3=reserved for TUB, 4=full mix (default: 0), i.e. clients auto-connect and hear all clients including themselves")
}

func main() {
	flag.Parse()

	boldRed := color.New(color.FgRed).Add(color.Bold)

	sess := session.Must(session.NewSessionWithOptions(session.Options{
		SharedConfigState: session.SharedConfigEnable,
		Config: aws.Config{
			Region: aws.String(region),
		},
	}))

	// Create a CloudWatchLogs client with additional configuration
	cwl := cloudwatchlogs.New(sess)
	ec2svc := ec2.New(sess)
	svc := ecs.New(sess)

	descSubnetsInput := &ec2.DescribeSubnetsInput{
		Filters: []*ec2.Filter{
			{
				Name: aws.String("tag:Name"),
				Values: []*string{
					aws.String("jacktrip"),
				},
			},
		},
	}
	subnets, err := ec2svc.DescribeSubnets(descSubnetsInput)
	if err != nil {
		panic(err)
	}

	descSGsInput := &ec2.DescribeSecurityGroupsInput{
		Filters: []*ec2.Filter{
			{
				Name: aws.String("tag:Name"),
				Values: []*string{
					aws.String("jacktrip"),
				},
			},
		},
	}
	securityGroups, err := ec2svc.DescribeSecurityGroups(descSGsInput)
	if err != nil {
		panic(err)
	}

	input := &ecs.RunTaskInput{
		Cluster:        aws.String("jacktrip"),
		TaskDefinition: aws.String("run-jacktrip"),
		LaunchType:     aws.String("FARGATE"),
		NetworkConfiguration: &ecs.NetworkConfiguration{
			AwsvpcConfiguration: &ecs.AwsVpcConfiguration{
				AssignPublicIp: aws.String("ENABLED"),
				SecurityGroups: []*string{aws.String(*securityGroups.SecurityGroups[0].GroupId)},
				Subnets:        []*string{aws.String(*subnets.Subnets[0].SubnetId)},
			},
		},
		Overrides: &ecs.TaskOverride{
			ContainerOverrides: []*ecs.ContainerOverride{
				&ecs.ContainerOverride{
					Name: aws.String("jacktrip"),
					Environment: []*ecs.KeyValuePair{
						&ecs.KeyValuePair{
							Name:  aws.String("HUB_PATCH"),
							Value: aws.String(strconv.Itoa(hubPatch)),
						},
					},
				},
			},
		},
	}
	result, err := svc.RunTask(input)

	if err != nil {
		panic(err)
	}

	task := result.Tasks[0]
	taskARN, err := arn.Parse(*task.TaskArn)

	logs := make(chan string)
	logStreamName := strings.Replace(taskARN.Resource, "task", "ecs", 1)

	totalLogLines := 0

	go func() {

		resp, err := cwl.GetLogEvents(&cloudwatchlogs.GetLogEventsInput{
			Limit:         aws.Int64(100),
			LogGroupName:  aws.String("/ecs/run-jacktrip"),
			LogStreamName: aws.String(logStreamName),
		})

		_, ok := err.(*cloudwatchlogs.ResourceNotFoundException)

		for ok {
			resp, err = cwl.GetLogEvents(&cloudwatchlogs.GetLogEventsInput{
				Limit:         aws.Int64(100),
				LogGroupName:  aws.String("/ecs/run-jacktrip"),
				LogStreamName: aws.String(logStreamName),
			})

			_, ok = err.(*cloudwatchlogs.ResourceNotFoundException)
		}

		resp, err = cwl.GetLogEvents(&cloudwatchlogs.GetLogEventsInput{
			Limit:         aws.Int64(100),
			LogGroupName:  aws.String("/ecs/run-jacktrip"),
			LogStreamName: aws.String(logStreamName),
		})

		totalLogLines += len(resp.Events)

		for _, event := range resp.Events {
			logs <- *event.Message
		}

		for {

			if totalLogLines > 0 {
				resp, err = cwl.GetLogEvents(&cloudwatchlogs.GetLogEventsInput{
					Limit:         aws.Int64(100),
					LogGroupName:  aws.String("/ecs/run-jacktrip"),
					LogStreamName: aws.String(logStreamName),
					NextToken:     aws.String(*resp.NextForwardToken),
				})
			} else {
				resp, err = cwl.GetLogEvents(&cloudwatchlogs.GetLogEventsInput{
					Limit:         aws.Int64(100),
					LogGroupName:  aws.String("/ecs/run-jacktrip"),
					LogStreamName: aws.String(logStreamName),
				})
			}
			totalLogLines += len(resp.Events)
			for _, event := range resp.Events {
				logs <- *event.Message
			}

		}
	}()

	taskArn := task.TaskArn

	descResult := WaitForDesiredState(svc, taskArn, "RUNNING")

	task = descResult.Tasks[0]

	go func() {

		descInput := &ecs.DescribeTasksInput{
			Cluster: aws.String("jacktrip"),
			Tasks:   []*string{taskArn},
		}

		descResult, err = svc.DescribeTasks(descInput)
		if err != nil {
			panic(err)
		}

		container := descResult.Tasks[0].Containers[0]
		for len(container.NetworkInterfaces) == 0 {
			descResult, err = svc.DescribeTasks(descInput)
			if err != nil {
				panic(err)
			}
		}

		// find attachment
		for _, attach := range descResult.Tasks[0].Attachments {
			if *attach.Type == "ElasticNetworkInterface" {
				for _, detail := range attach.Details {
					if *detail.Name == "networkInterfaceId" {
						descNIInput := &ec2.DescribeNetworkInterfacesInput{
							NetworkInterfaceIds: []*string{detail.Value},
						}

						descResult, err := ec2svc.DescribeNetworkInterfaces(descNIInput)
						if err != nil {
							panic(err)
						}

						publicIP := descResult.NetworkInterfaces[0].Association.PublicIp

						fmt.Printf("\nThis jacktrip server's IP is ")
						boldRed.Printf("%s\n", *publicIP)

					}
				}

			}
		}

	}()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)

	go func() {
		for range c {
			// sig is a ^C, handle it
			fmt.Printf("Stopping task %s !\n", taskARN.Resource)

			stopInput := &ecs.StopTaskInput{
				Cluster: aws.String("jacktrip"),
				Task:    aws.String(*taskArn),
			}

			_, err := svc.StopTask(stopInput)
			if err != nil {
				panic(err)
			}

			WaitForDesiredState(svc, taskArn, "STOPPED")

			fmt.Println("BYE!")
			os.Exit(0)
		}
	}()

	for {
		logLine := <-logs
		fmt.Printf(">>> %s\n", logLine)
	}

}

func WaitForDesiredState(svc *ecs.ECS, taskArn *string, desiredState string) *ecs.DescribeTasksOutput {
	taskARN, err := arn.Parse(*taskArn)

	fmt.Printf("Waiting for task %s to reach state %s ", taskARN.Resource, desiredState)

	descInput := &ecs.DescribeTasksInput{
		Cluster: aws.String("jacktrip"),
		Tasks:   []*string{taskArn},
	}

	result, err := svc.DescribeTasks(descInput)
	if err != nil {
		panic(err)
	}

	task := result.Tasks[0]
	status := task.LastStatus

	for *status != desiredState {
		result, err := svc.DescribeTasks(descInput)
		if err != nil {
			panic(err)
		}

		task = result.Tasks[0]
		status = task.LastStatus

		fmt.Printf(".")
		time.Sleep(time.Second)
	}

	fmt.Printf(" done!\n")
	return result
}
