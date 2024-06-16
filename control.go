package main

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/credentials/ec2rolecreds"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/endpoints"
	"github.com/aws/aws-sdk-go/aws/session"
	v4 "github.com/aws/aws-sdk-go/aws/signer/v4"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type ControlChannel struct {
	signer         *v4.Signer
	region         string
	mgsUrlHttp     string
	mgsWssUrl      string
	conn           *websocket.Conn
	queue          []*AgentMessage
	qmutex         sync.Mutex
	instanceId     string
	executionQueue []Execution
	eqMutex        sync.Mutex
}

func NewControlChannel() (*ControlChannel, error) {
	instanceId, err := QueryMetadata("instance-id")
	if err != nil {
		return nil, err
	}

	region, err := QueryMetadata("placement/region")
	if err != nil {
		return nil, err
	}

	sess, err := session.NewSession(aws.NewConfig().WithSTSRegionalEndpoint(endpoints.RegionalSTSEndpoint).WithRegion(region))
	if err != nil {
		return nil, err
	}
	creds := credentials.NewChainCredentials(
		[]credentials.Provider{
			&ec2rolecreds.EC2RoleProvider{
				Client: ec2metadata.New(sess),
			},
		})
	signer := v4.NewSigner(creds)

	return &ControlChannel{
		signer:         signer,
		mgsUrlHttp:     fmt.Sprintf("https://ssmmessages.us-west-2.amazonaws.com/v1/control-channel/%s?role=subscribe&stream=input", instanceId),
		mgsWssUrl:      fmt.Sprintf("wss://ssmmessages.us-west-2.amazonaws.com/v1/control-channel/%s?role=subscribe&stream=input", instanceId),
		region:         region,
		queue:          make([]*AgentMessage, 0),
		instanceId:     instanceId,
		executionQueue: make([]Execution, 0),
	}, nil
}

func (cc *ControlChannel) InitiateChannel() error {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = (&net.Dialer{
		KeepAlive: 0,
	}).DialContext

	controlChannelUid := uuid.New().String()
	createControlChannelInput := &CreateControlChannelInput{
		MessageSchemaVersion: aws.String("1.0"),
		RequestId:            aws.String(controlChannelUid),
	}
	jsonValue, _ := json.Marshal(createControlChannelInput)
	resp, err := makeRestcall(jsonValue, "POST", cc.mgsUrlHttp, "us-west-2", cc.signer)
	if err != nil {
		fmt.Printf("makeRestcall failed: %s", err)
		return err
	}
	var output CreateControlChannelOutput
	if resp != nil {
		if err = xml.Unmarshal(resp, &output); err != nil {
			fmt.Printf("createControlChannel response failed: %s", err)
			return err
		}
	}
	controlChannelDialerInput := &websocket.Dialer{
		Proxy:           http.ProxyFromEnvironment,
		WriteBufferSize: 142000,
	}

	request, _ := http.NewRequest("GET", cc.mgsWssUrl, nil)
	cc.signer.Sign(request, nil, "ssmmessages", cc.region, time.Now())
	conn, r, err := controlChannelDialerInput.Dial(cc.mgsWssUrl, request.Header)
	if err != nil {
		if resp != nil {
			fmt.Printf("Failed to dial websocket %v %v\n", r.Status, err)
		} else {
			fmt.Printf("Failed to dial websocket %v\n", err)
		}
	}
	cc.conn = conn

	openControlChannelInput := OpenControlChannelInput{
		MessageSchemaVersion:   aws.String("1.0"),
		RequestId:              aws.String(controlChannelUid),
		TokenValue:             aws.String(*output.TokenValue),
		AgentVersion:           aws.String("3.3.0.1"),
		PlatformType:           aws.String("linux"),
		RequireAcknowledgement: aws.Bool(true),
	}
	jsonValue, _ = json.Marshal(openControlChannelInput)
	err = conn.WriteMessage(websocket.TextMessage, jsonValue)
	if err != nil {
		fmt.Printf("Failed WriteMessage to open control channel %v\n", err)
	}
	return nil
}

func (cc *ControlChannel) ProcessIncomingMessages(done <-chan struct{}) error {
	go cc.processNextItem(done)
	go cc.executeNextItem(done)
	for {
		select {
		case <-done:
			fmt.Println("done channel closed. Exiting")
			return nil
		default:
			agentMessage, err := cc.getAgentMessage()
			if err != nil {
				fmt.Printf("getAgentMessage error %v\n", err)
				continue
			}
			if agentMessage == nil {
				fmt.Println("Finished processing control_channel_ready message")
				continue
			}
			cc.qmutex.Lock()
			cc.queue = append(cc.queue, agentMessage)
			cc.qmutex.Unlock()
		}
	}
}

func (cc *ControlChannel) processNextItem(done <-chan struct{}) error {
	for {
		select {
		case <-done:
			fmt.Println("done channel closed. Exiting")
			return nil
		default:
			if len(cc.queue) == 0 {
				continue
			}
			cc.qmutex.Lock()
			am := cc.queue[0]
			cc.queue = cc.queue[1:]
			cc.qmutex.Unlock()
			fmt.Printf("Got an item to process %v\n", am)
			cc.addToQueue(am)
		}
	}
}

func (cc *ControlChannel) executeNextItem(done <-chan struct{}) error {
	for {
		select {
		case <-done:
			fmt.Println("done channel closed. Exiting")
			return nil
		default:
			if len(cc.executionQueue) == 0 {
				continue
			}
			cc.eqMutex.Lock()
			ex := cc.executionQueue[0]
			cc.executionQueue = cc.executionQueue[1:]
			cc.eqMutex.Unlock()
			fmt.Printf("This is where the payload is passed to piezo or eks-init: %s, %v\n", ex.input.RunCommand, ex.input)
			fmt.Printf("Executing commandid: %s\n", ex.docState.DocumentInformation.CommandID)
			time.Sleep(1 * time.Minute)
			succ, err := constructMessage(&DocumentResult{
				DocumentName:    ex.documentName,
				LastPlugin:      "",
				MessageID:       ex.messageId,
				DocumentVersion: ex.documentVersion,
				Status:          ResultStatusSuccess,
				PluginResults: map[string]*PluginResult{
					"aws:runShellScript": &PluginResult{
						PluginID:       ex.input.ID,
						Code:           0,
						PluginName:     ex.pluginName,
						Status:         ResultStatusSuccess,
						Output:         "hehe",
						StartDateTime:  time.Now(),
						EndDateTime:    time.Now(),
						StepName:       "aws:runShellScript",
						StandardOutput: "out",
						StandardError:  "err",
					},
				},
			})
			if err != nil {
				fmt.Printf("constructMessage error: %v\n", err)
				continue
			}
			b, err := succ.Serialize()
			if err != nil {
				fmt.Printf("succ.Serialize() error: %v\n", err)
				continue
			}
			err = cc.conn.WriteMessage(websocket.BinaryMessage, b)
			if err != nil {
				fmt.Printf("conn.WriteMessage for Success error: %v\n", err)
			}
			payloadDoc2 := PrepareReplyPayloadToUpdateDocumentStatus(AgentInfo{
				Os:      "linux",
				Version: "3.3.0.1",
				Name:    "some-agent",
			}, ResultStatusSuccess, "", nil)
			ex.docState.DocumentInformation.DocumentStatus = ResultStatusSuccess
			sendDocResponse(payloadDoc2, ex.docState, cc.conn)
		}
	}
}

func (cc *ControlChannel) addToQueue(agentMessage *AgentMessage) {
	switch agentMessage.MessageType {
	case "agent_job":
		fmt.Printf("Received agent_job for processing %v\n", agentMessage)
		var agentJobPayload AgentJobPayload
		err := json.Unmarshal(agentMessage.Payload, &agentJobPayload)
		if err != nil {
			fmt.Printf("agentmessage error: %v\n", err)
			return
		}
		if !strings.HasPrefix(agentJobPayload.Topic, "aws.ssm.sendCommand") {
			fmt.Printf("%s does not match sendCommand topic\n", agentJobPayload.Topic)
			return
		}
		docState, err := parseAgentJobSendCommandMessage(agentJobPayload, cc.instanceId, *agentMessage)
		if err != nil {
			fmt.Printf("parseAgentJobSendCommandMessage error: %v\n", err)
			return
		}
		fmt.Printf("Processing doc %v\n", docState)

		err = buildAgentJobAckMessageAndSend(agentMessage.MessageId, docState.DocumentInformation.MessageID, agentMessage.CreatedDate, Successful, cc.conn)
		if err != nil {
			fmt.Printf("buildAgentJobAckMessageAndSend error: %v\n", err)
			return
		}

		payloadDoc := PrepareReplyPayloadToUpdateDocumentStatus(AgentInfo{
			Os:      "linux",
			Version: "3.3.0.1",
			Name:    "some-agent",
		}, ResultStatusInProgress, "", nil)
		sendDocResponse(payloadDoc, docState, cc.conn)

		pluginInfo := docState.InstancePluginsInformation[0]
		var pluginInput RunScriptPluginInput
		err = Remarshal(pluginInfo.Configuration.Properties, &pluginInput)
		if err != nil {
			fmt.Printf("Remarshal error: %v\n", err)
			return
		}
		fmt.Printf("RunScriptPluginInput %v\n", pluginInput)

		if pluginInfo.Configuration.PluginName != "aws:runShellScript" {
			fmt.Printf("Unknown plugin name: %s\n", pluginInfo.Configuration.PluginName)
			return
		}

		cc.eqMutex.Lock()

		cc.executionQueue = append(cc.executionQueue, Execution{
			commandId:       docState.DocumentInformation.CommandID,
			input:           &pluginInput,
			documentName:    docState.DocumentInformation.DocumentName,
			messageId:       docState.DocumentInformation.MessageID,
			documentVersion: docState.DocumentInformation.DocumentVersion,
			pluginName:      pluginInfo.Configuration.PluginID,
			docState:        docState,
		})
		cc.eqMutex.Unlock()
	case "agent_job_reply_ack":
		fmt.Printf("Received agent_job_reply_ack")
	}
}

// TODO: the first control_channel_ready should be available in 2s
func (cc *ControlChannel) getAgentMessage() (*AgentMessage, error) {
	fmt.Println("Waiting for message")
	messageType, rawMessage, err := cc.conn.ReadMessage()
	if err != nil {
		fmt.Printf("Received error on websocket: %v\n", err)
		if e, ok := err.(*websocket.CloseError); ok {
			fmt.Printf("received CloseError from websocker connection: %v\n", e.Error())
		} else {
			fmt.Printf("received expected error from websocker connection: %v\n", err)
		}
		return nil, err
	}
	fmt.Printf("Received Message %v %v \n", messageType, rawMessage)
	agentMessage := &AgentMessage{}
	if err := agentMessage.Deserialize(rawMessage); err != nil {
		fmt.Printf("ReadMessage deser failure %v\n", err)
		return nil, err
	}
	fmt.Printf("received message through control channel %v, message type: %s\n", agentMessage.MessageId, agentMessage.MessageType)
	if agentMessage.MessageType == "control_channel_ready" {
		return nil, nil
	}
	return agentMessage, nil
}

func (cc *ControlChannel) makeRestcall(request []byte, methodType string, url string, region string, signer *v4.Signer) ([]byte, error) {
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

// RunScriptPluginInput represents one set of commands executed by the RunScript plugin.
type RunScriptPluginInput struct {
	PluginInput
	RunCommand       []string
	Environment      map[string]string
	ID               string
	WorkingDirectory string
	TimeoutSeconds   interface{}
}

// PluginInput represents the input of the plugin.
type PluginInput struct {
}

// Remarshal marshals an object to Json then parses it back to another object.
// This is useful for example when we want to go from map[string]interface{}
// to a more specific struct type or if we want a deep copy of the object.
func Remarshal(obj interface{}, remarshalledObj interface{}) (err error) {
	b, err := json.Marshal(obj)
	if err != nil {
		return
	}
	err = json.Unmarshal(b, remarshalledObj)
	if err != nil {
		return
	}
	return nil
}

type Execution struct {
	commandId       string
	input           *RunScriptPluginInput
	documentName    string
	messageId       string
	documentVersion string
	pluginName      string
	docState        *DocumentState
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
