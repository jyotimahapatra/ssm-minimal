package main

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/credentials/ec2rolecreds"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/endpoints"
	"github.com/aws/aws-sdk-go/aws/session"
	v4 "github.com/aws/aws-sdk-go/aws/signer/v4"
	ssmv1 "github.com/aws/aws-sdk-go/service/ssm"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
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

	go func() {
		mgsUrlHttp := "https://ssmmessages.us-west-2.amazonaws.com/v1/control-channel/i-01cb171edb8920318?role=subscribe&stream=input"
		mgsUrl := "wss://ssmmessages.us-west-2.amazonaws.com/v1/control-channel/i-01cb171edb8920318?role=subscribe&stream=input"
		sess := session.New(&aws.Config{
			STSRegionalEndpoint: endpoints.RegionalSTSEndpoint,
			Region:              aws.String("us-west-2"),
		})
		creds := credentials.NewChainCredentials(
			[]credentials.Provider{
				&ec2rolecreds.EC2RoleProvider{
					Client: ec2metadata.New(sess),
				},
			})
		signer := v4.NewSigner(creds)
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.DialContext = (&net.Dialer{
			KeepAlive: 0,
		}).DialContext
		uid := uuid.New().String()

		createControlChannelInput := &CreateControlChannelInput{
			MessageSchemaVersion: aws.String("1.0"),
			RequestId:            aws.String(uid),
		}
		jsonValue, _ := json.Marshal(createControlChannelInput)
		resp, err := makeRestcall(jsonValue, "POST", mgsUrlHttp, "us-west-2", signer)
		if err != nil {
			fmt.Printf("createControlChannel request failed: %s", err)
			return
		}
		var output CreateControlChannelOutput
		if resp != nil {
			if err = xml.Unmarshal(resp, &output); err != nil {
				fmt.Printf("createControlChannel response failed: %s", err)
				return
			}
		}
		fmt.Printf("response %v\n", string(resp))

		controlChannelDialerInput := &websocket.Dialer{
			Proxy:           http.ProxyFromEnvironment,
			WriteBufferSize: 142000,
		}
		request, _ := http.NewRequest("GET", mgsUrl, nil)
		signer.Sign(request, nil, "ssmmessages", "us-west-2", time.Now())
		conn, r, err := controlChannelDialerInput.Dial(mgsUrl, request.Header)
		if err != nil {
			if resp != nil {
				fmt.Printf("Failed to dial websocket %v %v\n", r.Status, err)
			} else {
				fmt.Printf("Failed to dial websocket %v\n", err)
			}
		}
		go func() {
			for {
				fmt.Printf("Waiting for message")
				messageType, rawMessage, err := conn.ReadMessage()
				if err != nil {
					fmt.Printf("ReadMessage error %v \n", err)
					continue
				}
				fmt.Printf("ReadMessage reply %v %v \n", messageType, rawMessage)
			}
		}()
		openControlChannelInput := OpenControlChannelInput{
			MessageSchemaVersion:   aws.String("1.0"),
			RequestId:              aws.String(uid),
			TokenValue:             aws.String(*output.TokenValue),
			AgentVersion:           aws.String("3.3.0.1"),
			PlatformType:           aws.String("linux"),
			RequireAcknowledgement: aws.Bool(true),
		}
		jsonValue, _ = json.Marshal(openControlChannelInput)
		err = conn.WriteMessage(websocket.TextMessage, jsonValue)
		if err != nil {
			fmt.Printf("Failed to sending websocket ping %v\n", err)
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

func makeRestcall(request []byte, methodType string, url string, region string, signer *v4.Signer) ([]byte, error) {
	httpRequest, err := http.NewRequest(methodType, url, bytes.NewBuffer(request))
	if err != nil {
		return nil, fmt.Errorf("failed to create http request: %s", err)
	}

	httpRequest.Header.Set("Content-Type", "application/json")
	_, err = signer.Sign(httpRequest, bytes.NewReader(request), "ssmmessages", region, time.Now())
	if err != nil {
		return nil, fmt.Errorf("failed to sign the request: %s", err)
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	client := &http.Client{
		Timeout:   time.Second * 15,
		Transport: transport,
	}

	resp, err := client.Do(httpRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to make http client call: %s", err)
	}

	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read bytes from http response: %s", err)
	}

	if resp.StatusCode == 201 {
		return body, nil
	} else {
		return nil, fmt.Errorf("unexpected response from the service %s", body)
	}
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

type CreateControlChannelInput struct {
	_ struct{} `type:"structure"`

	// MessageSchemaVersion is a required field
	MessageSchemaVersion *string `json:"MessageSchemaVersion" min:"1" type:"string" required:"true"`

	// RequestId is a required field
	RequestId *string `json:"RequestId" min:"16" type:"string" required:"true"`
}

type CreateControlChannelOutput struct {
	_ struct{} `type:"structure"`

	// MessageSchemaVersion
	MessageSchemaVersion *string `xml:"MessageSchemaVersion"`

	// TokenValue is a required field
	TokenValue *string `xml:"TokenValue"`
}

type OpenControlChannelInput struct {
	_ struct{} `type:"structure"`

	// Cookie for reopening a channel
	Cookie *string `json:"Cookie" min:"1" type:"string"`

	// MessageSchemaVersion is a required field
	MessageSchemaVersion *string `json:"MessageSchemaVersion" min:"1" type:"string" required:"true"`

	// RequestId is a required field
	RequestId *string `json:"RequestId" min:"16" type:"string" required:"true"`

	// TokenValue is a required field
	TokenValue *string `json:"TokenValue" min:"1" type:"string" required:"true"`

	// AgentVersion is a required field
	AgentVersion *string `json:"AgentVersion" min:"1" type:"string" required:"true"`

	// PlatformType is a required field
	PlatformType *string `json:"PlatformType" min:"1" type:"string" required:"true"`

	// RequireAcknowledgement is a required field
	RequireAcknowledgement *bool `json:"RequireAcknowledgement" type:"boolean" required:"true"`
}
