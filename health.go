package main

import (
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/endpoints"
	"github.com/aws/aws-sdk-go/aws/session"
	ssmv1 "github.com/aws/aws-sdk-go/service/ssm"
)

func startUpdateAgentConnection(done <-chan struct{}) error {

	instanceId, err := QueryMetadata("instance-id")
	if err != nil {
		return err
	}
	az, err := QueryMetadata("placement/availability-zone")
	if err != nil {
		return err
	}
	azId, err := QueryMetadata("placement/availability-zone-id")
	if err != nil {
		return err
	}
	sess, err := session.NewSessionWithOptions(session.Options{
		Config: *aws.NewConfig().WithSTSRegionalEndpoint(endpoints.RegionalSTSEndpoint).WithRegion("us-west-2"),
	})
	if err != nil {
		fmt.Printf("error in session creation: %v\n", err)
	}

	ssmClient := ssmv1.New(sess)
	agentStatus := ssmv1.UpdateInstanceInformationInput{
		AgentName:            aws.String("amazon-ssm-agent"),
		AgentStatus:          aws.String("Active"),
		AgentVersion:         aws.String("3.3.0.1"),
		AvailabilityZone:     aws.String(az),
		AvailabilityZoneId:   aws.String(azId),
		SSMConnectionChannel: aws.String(""),
		InstanceId:           aws.String(instanceId),
	}
	go periodicUpdateAgentStatus(done, agentStatus, ssmClient)
}

func periodicUpdateAgentStatus(done <-chan struct{}, agentStatus ssmv1.UpdateInstanceInformationInput, ssmClient *ssmv1.SSM) {
	ticker := time.NewTicker(60 * time.Second)
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			_, err := ssmClient.UpdateInstanceInformation(&agentStatus)
			if err != nil {
				fmt.Printf("error in UpdateInstanceInformation: %v\n", err)
			}
		}
	}
}
