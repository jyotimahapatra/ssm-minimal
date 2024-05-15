package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/endpoints"
	"github.com/aws/aws-sdk-go/aws/session"
	ssmv1 "github.com/aws/aws-sdk-go/service/ssm"
)

func main() {
	fmt.Println("hello")
	done := make(chan bool, 1)
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		sess := session.New(&aws.Config{
			STSRegionalEndpoint: endpoints.RegionalSTSEndpoint,
			Region:              aws.String("us-west-2"),
		})
		cl1 := ssmv1.New(sess)
		params := ssmv1.UpdateInstanceInformationInput{
			AgentName:            aws.String("amazon-ssm-agent"),
			AgentStatus:          aws.String("Active"),
			AgentVersion:         aws.String("3.3.0.1"),
			AvailabilityZone:     aws.String("us-west-2c"),
			AvailabilityZoneId:   aws.String("usw2-az3"),
			SSMConnectionChannel: aws.String(""),
			InstanceId:           aws.String("i-01cb171edb8920318"),
		}
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				out, err := cl1.UpdateInstanceInformation(&params)
				fmt.Println(out)
				fmt.Println("------------")
				fmt.Println(err)
			}
		}
	}()
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigs
		done <- true
	}()
	<-done
}

/**
{
      "InstanceId": "i-01cb171edb8920318",
      "PingStatus": "Online",
      "LastPingDateTime": "2024-05-14T23:10:07.595000-07:00",
      "AgentVersion": "3.3.0.0",
      "IsLatestVersion": false,
      "PlatformType": "Linux",
      "PlatformName": "Amazon Linux",
      "PlatformVersion": "2",
      "ResourceType": "EC2Instance",
      "IPAddress": "172.16.104.210",
      "ComputerName": "ip-172-16-104-210.us-west-2.compute.internal",
      "AssociationStatus": "Failed",
      "LastAssociationExecutionDate": "2024-05-14T23:01:59-07:00",
      "LastSuccessfulAssociationExecutionDate": "2024-05-02T23:01:19-07:00",
      "AssociationOverview": {
        "DetailedStatus": "Failed",
        "InstanceAssociationStatusAggregatedCount": {
          "Failed": 4,
          "Success": 1
        }
      },
      "SourceId": "i-01cb171edb8920318",
      "SourceType": "AWS::EC2::Instance"
    }
*/
