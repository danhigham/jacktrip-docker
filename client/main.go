package main

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/arn"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ecs"
)

func main() {
	sess := session.Must(session.NewSessionWithOptions(session.Options{
		SharedConfigState: session.SharedConfigEnable,
		Config: aws.Config{
			Region: aws.String("eu-west-2"),
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
					aws.String("Main"),
				},
			},
		},
	}
	subnets, err := ec2svc.DescribeSubnets(descSubnetsInput)

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
