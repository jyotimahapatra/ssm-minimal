package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

func main() {
	fmt.Println("hello")
	done := make(chan struct{}, 1)
	startUpdateAgentConnection(done)
	cc, err := NewControlChannel()
	if err != nil {
		panic("control channel creation failed")
	}
	err = cc.InitiateChannel()
	if err != nil {
		panic("control channel init failed")
	}

	cc.ProcessIncomingMessages(done)

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigs
		done <- struct{}{}
	}()
	<-done
}

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

const (
	documentContent  = "DocumentContent"
	runtimeConfig    = "runtimeConfig"
	cloudwatchPlugin = "aws:cloudWatch"
	properties       = "properties"
	parameters       = "Parameters"
)

// AgentJobPayload parallels the structure of a send-command or cancel-command job
type AgentJobPayload struct {
	Payload       string `json:"Content"`
	JobId         string `json:"JobId"`
	Topic         string `json:"Topic"`
	SchemaVersion int    `json:"SchemaVersion"`
}

// AgentJobReplyContent parallels the structure of a send-command or cancel-command job
type AgentJobReplyContent struct {
	SchemaVersion int    `json:"schemaVersion"`
	JobId         string `json:"jobId"`
	Content       string `json:"content"`
	Topic         string `json:"topic"`
}

// AgentJobAck is the acknowledge message sent back to MGS for AgentJobs
type AgentJobAck struct {
	JobId        string `json:"jobId"`
	MessageId    string `json:"acknowledgedMessageId"`
	CreatedDate  string `json:"createdDate"`
	StatusCode   string `json:"statusCode"`
	ErrorMessage string `json:"errorMessage"`
}

type DocumentState struct {
	DocumentInformation        DocumentInfo
	DocumentType               DocumentType
	SchemaVersion              string
	InstancePluginsInformation []PluginState
	CancelInformation          CancelCommandInfo
	IOConfig                   IOConfiguration
	UpstreamServiceName        UpstreamServiceName
}

type DocumentInfo struct {
	// DocumentID is a unique name for file system
	// For Association, DocumentID = AssociationID.RunID
	// For RunCommand, DocumentID = CommandID
	// For Session, DocumentId = SessionId
	DocumentID      string
	CommandID       string
	AssociationID   string
	InstanceID      string
	MessageID       string
	RunID           string
	CreatedDate     string
	DocumentName    string
	DocumentVersion string
	DocumentStatus  ResultStatus
	RunCount        int
	ProcInfo        OSProcInfo
	ClientId        string
	RunAsUser       string
	SessionOwner    string
}

type DocumentType string

// PluginState represents information stored as interim state for any plugin
// This has both the configuration with which a plugin gets executed and a
// corresponding plugin result.
type PluginState struct {
	Configuration Configuration
	Name          string
	//TODO truncate this struct
	Result PluginResult
	Id     string
}

// CancelCommandInfo represents information relevant to a cancel-command that agent receives
// TODO  This might be revisited when Agent-cli is written to list previously executed commands
type CancelCommandInfo struct {
	CancelMessageID string
	CancelCommandID string
	Payload         string
	DebugInfo       string
}

// IOConfiguration represents information relevant to the output sources of a command
type IOConfiguration struct {
	OrchestrationDirectory string
	OutputS3BucketName     string
	OutputS3KeyPrefix      string
	CloudWatchConfig       CloudWatchConfiguration
}

// ResultStatus provides the granular status of a plugin.
// These are internal states maintained by agent during the execution of a command/config
type ResultStatus string

const (
	// ResultStatusNotStarted represents NotStarted status
	ResultStatusNotStarted ResultStatus = "NotStarted"
	// ResultStatusInProgress represents InProgress status
	ResultStatusInProgress ResultStatus = "InProgress"
	// ResultStatusSuccess represents Success status
	ResultStatusSuccess ResultStatus = "Success"
	// ResultStatusSuccessAndReboot represents SuccessAndReboot status
	ResultStatusSuccessAndReboot ResultStatus = "SuccessAndReboot"
	// ResultStatusPassedAndReboot represents PassedAndReboot status
	ResultStatusPassedAndReboot ResultStatus = "PassedAndReboot"
	// ResultStatusFailed represents Failed status
	ResultStatusFailed ResultStatus = "Failed"
	// ResultStatusCancelled represents Cancelled status
	ResultStatusCancelled ResultStatus = "Cancelled"
	// ResultStatusTimedOut represents TimedOut status
	ResultStatusTimedOut ResultStatus = "TimedOut"
	// ResultStatusSkipped represents Skipped status
	ResultStatusSkipped ResultStatus = "Skipped"
	// ResultStatusTestFailure represents test failure
	ResultStatusTestFailure ResultStatus = "TestFailure"
	// ResultStatusTestPass represents test passing
	ResultStatusTestPass ResultStatus = "TestPass"
)

// OSProcInfo represents information about the new process for outofproc
type OSProcInfo struct {
	Pid       int
	StartTime time.Time
}

// CloudWatchConfiguration represents information relevant to command output in cloudWatch
type CloudWatchConfiguration struct {
	LogGroupName              string
	LogStreamPrefix           string
	LogGroupEncryptionEnabled bool
}

// PluginResult represents a plugin execution result.
type PluginResult struct {
	PluginID           string       `json:"pluginID"`
	PluginName         string       `json:"pluginName"`
	Status             ResultStatus `json:"status"`
	Code               int          `json:"code"`
	Output             interface{}  `json:"output"`
	StartDateTime      time.Time    `json:"startDateTime"`
	EndDateTime        time.Time    `json:"endDateTime"`
	OutputS3BucketName string       `json:"outputS3BucketName"`
	OutputS3KeyPrefix  string       `json:"outputS3KeyPrefix"`
	StepName           string       `json:"stepName"`
	Error              string       `json:"error"`
	StandardOutput     string       `json:"standardOutput"`
	StandardError      string       `json:"standardError"`
}

// Configuration represents a plugin configuration as in the json format.
type Configuration struct {
	Settings                    interface{}
	Properties                  interface{}
	OutputS3KeyPrefix           string
	OutputS3BucketName          string
	S3EncryptionEnabled         bool
	CloudWatchLogGroup          string
	CloudWatchEncryptionEnabled bool
	CloudWatchStreamingEnabled  bool
	OrchestrationDirectory      string
	MessageId                   string
	BookKeepingFileName         string
	PluginName                  string
	PluginID                    string
	DefaultWorkingDirectory     string
	Preconditions               map[string][]PreconditionArgument
	IsPreconditionEnabled       bool
	CurrentAssociations         []string
	SessionId                   string
	ClientId                    string
	KmsKeyId                    string
	RunAsEnabled                bool
	RunAsUser                   string
	ShellProfile                ShellProfileConfig
	SessionOwner                string
	UpstreamServiceName         UpstreamServiceName
}

// ShellProfileConfig stores shell profile config
type ShellProfileConfig struct {
	Windows string `json:"windows" yaml:"windows"`
	Linux   string `json:"linux" yaml:"linux"`
}

// UpstreamServiceName defines which upstream service (MGS or MDS) the document request came from
type UpstreamServiceName string

const (
	// MessageGatewayService represents messages that came from MGS
	MessageGatewayService UpstreamServiceName = "MessageGatewayService"
	// MessageDeliveryService represents messages that came from MDS
	MessageDeliveryService UpstreamServiceName = "MessageDeliveryService"
)

// PreconditionArgument represents a single input value for the plugin precondition operators
// InitialArgumentValue contains the original value of the argument as specified by the user (e.g. "parameter: {{ paramName }}")
// ResolvedArgumentValue contains the value of the argument with resolved document parameters (e.g. "parameter: paramValue")
type PreconditionArgument struct {
	InitialArgumentValue  string
	ResolvedArgumentValue string
}

func toInstanceMessage(instanceId string, agentMessage AgentMessage, agentJobPayload AgentJobPayload) InstanceMessage {
	return InstanceMessage{
		CreatedDate: time.Unix(int64(agentMessage.CreatedDate), 0).String(),
		Destination: instanceId,
		MessageId:   agentJobPayload.JobId,
		Payload:     agentJobPayload.Payload,
		Topic:       agentJobPayload.Topic,
	}
}

// InstanceMessage is an interface between agent and both upstream services - MDS, MGS
// Messages from MDS (ssmmds.Message) and MGS (AgentPayload) will be converted to InstanceMessage
// ssmmds.Message, AgentPayload (upstream) --> InstanceMessage --> SendCommandPayload, CancelCommandPayload (agent)
type InstanceMessage struct {
	CreatedDate string
	Destination string
	MessageId   string
	Payload     string
	Topic       string
}

func parseAgentJobSendCommandMessage(agentJobPayload AgentJobPayload, instanceId string, agentMessage AgentMessage) (*DocumentState, error) {
	message := toInstanceMessage(instanceId, agentMessage, agentJobPayload)
	return ParseSendCommandMessage(message, MessageGatewayService)
}

// ParseSendCommandMessage parses send command message
func ParseSendCommandMessage(msg InstanceMessage, upstreamService UpstreamServiceName) (*DocumentState, error) {
	// parse message to retrieve parameters
	var parsedMessage SendCommandPayload
	err := json.Unmarshal([]byte(msg.Payload), &parsedMessage)
	if err != nil {
		errorMsg := fmt.Errorf("encountered error while parsing input - internal error %v", err)
		return nil, errorMsg
	}

	// adapt plugin configuration format from MDS to plugin expected format
	s3KeyPrefix := path.Join(parsedMessage.OutputS3KeyPrefix, parsedMessage.CommandID, msg.Destination)

	cloudWatchConfig, err := generateCloudWatchConfigFromPayload(parsedMessage)
	if err != nil {
		fmt.Println("err in cw config")
	}

	//messageOrchestrationDirectory := filepath.Join(messagesOrchestrationRootDir, commandID)

	documentType := SendCommand
	documentInfo := newDocumentInfo(msg, parsedMessage)
	parserInfo := DocumentParserInfo{
		OrchestrationDir: "",
		S3Bucket:         parsedMessage.OutputS3BucketName,
		S3Prefix:         s3KeyPrefix,
		MessageId:        documentInfo.MessageID,
		DocumentId:       documentInfo.DocumentID,
		CloudWatchConfig: cloudWatchConfig,
	}

	docContent := &DocContent{
		SchemaVersion: parsedMessage.DocumentContent.SchemaVersion,
		Description:   parsedMessage.DocumentContent.Description,
		RuntimeConfig: parsedMessage.DocumentContent.RuntimeConfig,
		MainSteps:     parsedMessage.DocumentContent.MainSteps,
		Parameters:    parsedMessage.DocumentContent.Parameters}

	//Data format persisted in Current Folder is defined by the struct - CommandState
	docState, err := InitializeDocState(documentType, docContent, documentInfo, parserInfo, parsedMessage.Parameters)
	if err != nil {
		return nil, err
	}
	docState.UpstreamServiceName = upstreamService

	return &docState, nil
}

// Marshal marshals an object to a json string.
// Returns empty string if marshal fails.
func Marshal(obj interface{}) (result string, err error) {
	var resultB []byte
	resultB, err = json.Marshal(obj)
	if err != nil {
		return
	}
	result = string(resultB)
	return
}

func GetCommandID(messageID string) (string, error) {
	//messageID format: E.g (aws.ssm.2b196342-d7d4-436e-8f09-3883a1116ac3.i-57c0a7be)
	if match, err := regexp.MatchString("aws\\.ssm\\..+\\.+", messageID); !match {
		return messageID, fmt.Errorf("invalid messageID format: %v | %v", messageID, err)
	}

	return getCommandID(messageID), nil
}

// getCommandID gets CommandID from given MessageID
func getCommandID(messageID string) string {
	// MdsMessageID is in the format of : aws.ssm.CommandId.InstanceId
	// E.g (aws.ssm.2b196342-d7d4-436e-8f09-3883a1116ac3.i-57c0a7be)
	mdsMessageIDSplit := strings.Split(messageID, ".")
	return mdsMessageIDSplit[len(mdsMessageIDSplit)-2]
}

type SendCommandPayload struct {
	Parameters              map[string]interface{} `json:"Parameters"`
	DocumentContent         DocumentContent        `json:"DocumentContent"`
	CommandID               string                 `json:"CommandId"`
	DocumentName            string                 `json:"DocumentName"`
	OutputS3KeyPrefix       string                 `json:"OutputS3KeyPrefix"`
	OutputS3BucketName      string                 `json:"OutputS3BucketName"`
	CloudWatchLogGroupName  string                 `json:"CloudWatchLogGroupName"`
	CloudWatchOutputEnabled string                 `json:"CloudWatchOutputEnabled"`
}

// DocumentContent object which represents ssm document content.
type DocumentContent struct {
	SchemaVersion string                   `json:"schemaVersion" yaml:"schemaVersion"`
	Description   string                   `json:"description" yaml:"description"`
	RuntimeConfig map[string]*PluginConfig `json:"runtimeConfig" yaml:"runtimeConfig"`
	MainSteps     []*InstancePluginConfig  `json:"mainSteps" yaml:"mainSteps"`
	Parameters    map[string]*Parameter    `json:"parameters" yaml:"parameters"`

	// InvokedPlugin field is set when document is invoked from any other plugin.
	// Currently, InvokedPlugin is set only in runDocument Plugin
	InvokedPlugin string
}

// PluginConfig stores plugin configuration
type PluginConfig struct {
	Settings    interface{} `json:"settings" yaml:"settings"`
	Properties  interface{} `json:"properties" yaml:"properties"`
	Description string      `json:"description" yaml:"description"`
}

// InstancePluginConfig stores plugin configuration
type InstancePluginConfig struct {
	Action        string              `json:"action" yaml:"action"` // plugin name
	Inputs        interface{}         `json:"inputs" yaml:"inputs"` // Properties
	MaxAttempts   int                 `json:"maxAttempts" yaml:"maxAttempts"`
	Name          string              `json:"name" yaml:"name"` // unique identifier
	OnFailure     string              `json:"onFailure" yaml:"onFailure"`
	Settings      interface{}         `json:"settings" yaml:"settings"`
	Timeout       int                 `json:"timeoutSeconds" yaml:"timeoutSeconds"`
	Preconditions map[string][]string `json:"precondition" yaml:"precondition"`
}

// A Parameter in the DocumentContent of an MDS message.
type Parameter struct {
	DefaultVal     interface{} `json:"default" yaml:"default"`
	Description    string      `json:"description" yaml:"description"`
	ParamType      string      `json:"type" yaml:"type"`
	AllowedVal     []string    `json:"allowedValues" yaml:"allowedValues"`
	AllowedPattern string      `json:"allowedPattern" yaml:"allowedPattern"`
	MinChars       json.Number `json:"minChars,omitempty" yaml:"minChars,omitempty"`
	MaxChars       json.Number `json:"maxChars,omitempty" yaml:"maxChars,omitempty"`
	MinItems       json.Number `json:"minItems,omitempty" yaml:"minItems,omitempty"`
	MaxItems       json.Number `json:"maxItems,omitempty" yaml:"maxItems,omitempty"`
}

func generateCloudWatchConfigFromPayload(parsedMessage SendCommandPayload) (CloudWatchConfiguration, error) {
	cloudWatchOutputEnabled, err := strconv.ParseBool(parsedMessage.CloudWatchOutputEnabled)
	cloudWatchConfig := CloudWatchConfiguration{}
	if err != nil || !cloudWatchOutputEnabled {
		return cloudWatchConfig, err
	}
	cloudWatchConfig.LogStreamPrefix, err = generateCloudWatchLogStreamPrefix(parsedMessage.CommandID)
	if err != nil {
		return cloudWatchConfig, err
	}
	if parsedMessage.CloudWatchLogGroupName != "" {
		cloudWatchConfig.LogGroupName = parsedMessage.CloudWatchLogGroupName
	} else {
		CloudWatchLogGroupNamePrefix := "/aws/ssm/"
		logGroupName := fmt.Sprintf("%s%s", CloudWatchLogGroupNamePrefix, parsedMessage.DocumentName)
		cloudWatchConfig.LogGroupName = cleanupLogGroupName(logGroupName)
	}
	return cloudWatchConfig, nil
}

// generateCloudWatchLogStreamPrefix creates the LogStreamPrefix for cloudWatch output. LogStreamPrefix = <CommandID>/<InstanceID>
func generateCloudWatchLogStreamPrefix(commandID string) (string, error) {
	return fmt.Sprintf("%s", commandID), nil
}

func cleanupLogGroupName(logGroupName string) string {
	// log group pattern referred from below URL
	// https://docs.aws.amazon.com/AmazonCloudWatchLogs/latest/APIReference/API_CreateLogGroup.html
	if reg, err := regexp.Compile(`[^a-zA-Z0-9_\-/\.#]`); reg != nil && err == nil {
		// replace invalid chars with dot(.)
		return reg.ReplaceAllString(logGroupName, ".")
	}
	return logGroupName
}

// DocumentParserInfo represents the parsed information from the request
type DocumentParserInfo struct {
	OrchestrationDir  string
	S3Bucket          string
	S3Prefix          string
	MessageId         string
	DocumentId        string
	DefaultWorkingDir string
	CloudWatchConfig  CloudWatchConfiguration
}

// newDocumentInfo initializes new DocumentInfo object
func newDocumentInfo(msg InstanceMessage, parsedMsg SendCommandPayload) DocumentInfo {

	documentInfo := new(DocumentInfo)

	documentInfo.CommandID, _ = GetCommandID(msg.MessageId)
	documentInfo.DocumentID = documentInfo.CommandID
	documentInfo.InstanceID = msg.Destination
	documentInfo.MessageID = msg.MessageId
	documentInfo.RunID = ToIsoDashUTC(time.Now())
	documentInfo.CreatedDate = msg.CreatedDate
	documentInfo.DocumentName = parsedMsg.DocumentName
	documentInfo.DocumentStatus = ResultStatusInProgress

	return *documentInfo
}

// ToIsoDashUTC converts a time into a string in Iso format yyyy-MM-ddTHH-mm-ss.fffZ in UTC timezone.
func ToIsoDashUTC(t time.Time) string {
	t = t.UTC()
	return fmt.Sprintf("%04d-%02d-%02dT%02d-%02d-%02d.%03dZ", t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond()/1000000)
}

const (
	// SendCommand represents document type for send command
	SendCommand DocumentType = "SendCommand"
	// CancelCommand represents document type for cancel command
	CancelCommand DocumentType = "CancelComamnd"
	// Association represents document type for association
	Association DocumentType = "Association"
	// StartSession represents document type for start session
	StartSession DocumentType = "StartSession"
	// TerminateSession represents document type for terminate session
	TerminateSession DocumentType = "TerminateSession"
	// SendCommandOffline represents document type for send command received from offline service
	SendCommandOffline DocumentType = "SendCommandOffline"
	// CancelCommandOffline represents document type for cancel command received from offline service
	CancelCommandOffline DocumentType = "CancelCommandOffline"
)

type DocContent DocumentContent

// GetSchemaVersion is a method used to get document schema version
func (docContent *DocContent) GetSchemaVersion() string {
	return docContent.SchemaVersion
}

// GetIOConfiguration is a method used to get IO config from the document
func (docContent *DocContent) GetIOConfiguration(parserInfo DocumentParserInfo) IOConfiguration {
	return IOConfiguration{
		OrchestrationDirectory: parserInfo.OrchestrationDir,
		OutputS3BucketName:     parserInfo.S3Bucket,
		OutputS3KeyPrefix:      parserInfo.S3Prefix,
		CloudWatchConfig:       parserInfo.CloudWatchConfig,
	}
}

// ParseDocument is a method used to parse documents that are not received by any service (MDS or State manager)
func (docContent *DocContent) ParseDocument(
	docInfo DocumentInfo,
	parserInfo DocumentParserInfo,
	params map[string]interface{}) (pluginsInfo []PluginState, err error) {

	return parseDocumentContent(*docContent, parserInfo, params)
}

// InitializeDocState is a method to obtain the state of the document.
// This method calls into ParseDocument to obtain the InstancePluginInformation
func InitializeDocState(
	documentType DocumentType,
	docContent IDocumentContent,
	docInfo DocumentInfo,
	parserInfo DocumentParserInfo,
	params map[string]interface{}) (docState DocumentState, err error) {

	docState.SchemaVersion = docContent.GetSchemaVersion()
	docState.DocumentType = documentType
	docState.DocumentInformation = docInfo
	docState.IOConfig = docContent.GetIOConfiguration(parserInfo)

	pluginInfo, err := docContent.ParseDocument(docInfo, parserInfo, params)
	if err != nil {
		return
	}
	docState.InstancePluginsInformation = pluginInfo
	return docState, nil
}

type IDocumentContent interface {
	GetSchemaVersion() string
	GetIOConfiguration(parserInfo DocumentParserInfo) IOConfiguration
	ParseDocument(docInfo DocumentInfo, parserInfo DocumentParserInfo, params map[string]interface{}) (pluginsInfo []PluginState, err error)
}

// ValidParameters checks if parameter names are valid. Returns valid parameters only.
func ValidParameters(params map[string]interface{}) map[string]interface{} {
	validParams := make(map[string]interface{})
	for paramName, paramValue := range params {
		if validName(paramName) {
			validParams[paramName] = paramValue
		} else {
			fmt.Println("invalid parameter name %v", paramName)
		}
	}
	return validParams
}

// validName checks whether the given parameter name is valid.
func validName(paramName string) bool {
	const paramNameRegex = "^[a-zA-Z0-9]+$"
	paramNameValidator := regexp.MustCompile(paramNameRegex)
	return paramNameValidator.MatchString(paramName)
}

// parseDocumentContent parses an SSM Document and returns the plugin information
func parseDocumentContent(docContent DocContent, parserInfo DocumentParserInfo, params map[string]interface{}) (pluginsInfo []PluginState, err error) {

	switch docContent.SchemaVersion {
	case "1.0", "1.2":
		return parsePluginStateForV10Schema(docContent, parserInfo.OrchestrationDir, parserInfo.S3Bucket, parserInfo.S3Prefix, parserInfo.MessageId, parserInfo.DocumentId, parserInfo.DefaultWorkingDir)

	case "2.0", "2.0.1", "2.0.2", "2.0.3", "2.2":

		return parsePluginStateForV20Schema(docContent, parserInfo.OrchestrationDir, parserInfo.S3Bucket, parserInfo.S3Prefix, parserInfo.MessageId, parserInfo.DocumentId, parserInfo.DefaultWorkingDir, params)

	default:
		return pluginsInfo, fmt.Errorf("Unsupported document")
	}
}

// parsePluginStateForV10Schema initializes pluginsInfo for the docState. Used for document v1.0 and 1.2
func parsePluginStateForV10Schema(
	docContent DocContent,
	orchestrationDir, s3Bucket, s3Prefix, messageID, documentID, defaultWorkingDir string) (pluginsInfo []PluginState, err error) {

	if len(docContent.RuntimeConfig) == 0 {
		return pluginsInfo, fmt.Errorf("Unsupported schema format")
	}
	//initialize plugin states as map
	pluginsInfo = []PluginState{}
	// getPluginConfigurations converts from PluginConfig (structure from the MDS message) to plugin.Configuration (structure expected by the plugin)
	pluginConfigurations := []*Configuration{}
	for pluginName, pluginConfig := range docContent.RuntimeConfig {
		config := Configuration{
			Settings:                pluginConfig.Settings,
			Properties:              pluginConfig.Properties,
			OutputS3BucketName:      s3Bucket,
			OutputS3KeyPrefix:       "",
			OrchestrationDirectory:  "",
			MessageId:               messageID,
			BookKeepingFileName:     documentID,
			PluginName:              pluginName,
			PluginID:                pluginName,
			DefaultWorkingDirectory: defaultWorkingDir,
		}
		pluginConfigurations = append(pluginConfigurations, &config)
	}

	for _, value := range pluginConfigurations {
		var plugin PluginState
		plugin.Configuration = *value
		plugin.Id = value.PluginID
		plugin.Name = value.PluginName
		pluginsInfo = append(pluginsInfo, plugin)
	}
	return
}

// parsePluginStateForV20Schema initializes instancePluginsInfo for the docState. Used by document v2.0.
func parsePluginStateForV20Schema(
	docContent DocContent,
	orchestrationDir, s3Bucket, s3Prefix, messageID, documentID, defaultWorkingDir string, params map[string]interface{}) (pluginsInfo []PluginState, err error) {

	if len(docContent.MainSteps) == 0 {
		return pluginsInfo, fmt.Errorf("Unsupported schema format")
	}
	//initialize plugin states as array
	pluginsInfo = []PluginState{}

	// set precondition flag based on document schema version
	isPreconditionEnabled := false

	// getPluginConfigurations converts from PluginConfig (structure from the MDS message) to plugin.Configuration (structure expected by the plugin)
	for _, instancePluginConfig := range docContent.MainSteps {
		pluginName := instancePluginConfig.Action
		config := Configuration{
			Settings:                instancePluginConfig.Settings,
			Properties:              instancePluginConfig.Inputs,
			OutputS3BucketName:      s3Bucket,
			OutputS3KeyPrefix:       "",
			OrchestrationDirectory:  "",
			MessageId:               messageID,
			BookKeepingFileName:     documentID,
			PluginName:              pluginName,
			PluginID:                instancePluginConfig.Name,
			Preconditions:           map[string][]PreconditionArgument{},
			IsPreconditionEnabled:   isPreconditionEnabled,
			DefaultWorkingDirectory: defaultWorkingDir,
		}

		var plugin PluginState
		plugin.Configuration = config
		plugin.Id = config.PluginID
		plugin.Name = config.PluginName
		pluginsInfo = append(pluginsInfo, plugin)
	}
	return
}

func buildAgentJobAckMessageAndSend(ackMessageId uuid.UUID, jobId string, createdDate uint64, errorCode ErrorCode, conn *websocket.Conn) error {
	ackMsg := &AgentJobAck{
		JobId:        jobId,
		MessageId:    ackMessageId.String(),
		CreatedDate:  toISO8601(createdDate),
		StatusCode:   string(Successful),
		ErrorMessage: string(errorCode),
	}

	replyBytes, err := json.Marshal(ackMsg)
	if err != nil {
		fmt.Printf("marshal error: %v\n", err)
		return err
	}
	replyUUID := uuid.New()
	agentMessage := &AgentMessage{
		MessageType:    "agent_job_ack",
		SchemaVersion:  1,
		CreatedDate:    uint64(time.Now().UnixNano() / 1000000),
		SequenceNumber: 0,
		Flags:          0,
		MessageId:      replyUUID,
		Payload:        replyBytes,
	}

	msg, err := agentMessage.Serialize()
	if err != nil {
		fmt.Printf("serialize error: %v\n", err)
		return err
	}

	if err = conn.WriteMessage(websocket.BinaryMessage, msg); err != nil {
		fmt.Printf("WriteMessage error: %v\n", err)
		return err
	}
	fmt.Println("sent ack")
	return nil
}

// convert uint64 to ISO-8601 time stamp
func toISO8601(createdDate uint64) string {
	timeVal := time.Unix(0, int64(createdDate)*int64(time.Millisecond)).UTC()
	ISO8601Format := "2006-01-02T15:04:05.000Z"
	return timeVal.Format(ISO8601Format)
}

type ErrorCode string

const (
	// Name represents name of the service
	Name = "MessageHandler"

	// UnexpectedDocumentType represents that message handler received unexpected document type
	UnexpectedDocumentType ErrorCode = "UnexpectedDocumentType"

	// idempotencyFileDeletionTimeout represents file deletion timeout after persisting command for idempotency
	idempotencyFileDeletionTimeout = 10

	// ProcessorBufferFull represents that the processor buffer is full
	ProcessorBufferFull ErrorCode = "ProcessorBufferFull"

	// ClosedProcessor represents that the processor is closed
	ClosedProcessor ErrorCode = "ClosedProcessor"

	// ProcessorErrorCodeTranslationFailed represents that the processor to message handler error code translation failed
	ProcessorErrorCodeTranslationFailed ErrorCode = "ProcessorErrorCodeTranslationFailed"

	// DuplicateCommand represents duplicate command in the buffer
	DuplicateCommand ErrorCode = "DuplicateCommand"

	// InvalidDocument represents invalid document received in processor
	InvalidDocument ErrorCode = "InvalidDocument"

	// ContainerNotSupported represents agent job messages are not supported for containers
	ContainerNotSupported ErrorCode = "ContainerNotSupported"

	// AgentJobMessageParseError represents agent job messages cannot be parsed to Document State
	AgentJobMessageParseError ErrorCode = "AgentJobMessageParseError"

	// UnexpectedError represents unexpected error
	UnexpectedError ErrorCode = "UnexpectedError"

	// Successful represent agent job messages can be processed successfully
	Successful ErrorCode = "Successful"
)

// Serialize serializes AgentMessage message into a byte array.
// * Payload is a variable length byte data.
// * | HL|         MessageType           |Ver|  CD   |  Seq  | Flags |
// * |         MessageId                     |           Digest              |PayType| PayLen|
// * |         Payload      			|
func (agentMessage *AgentMessage) Serialize() (result []byte, err error) {
	payloadLength := uint32(len(agentMessage.Payload))
	headerLength := uint32(AgentMessage_PayloadLengthOffset)
	// If the payloadinfo length is incorrect, fix it.
	if payloadLength != agentMessage.PayloadLength {
		agentMessage.PayloadLength = payloadLength
	}

	totalMessageLength := headerLength + AgentMessage_PayloadLengthLength + payloadLength
	result = make([]byte, totalMessageLength)

	if err = putUInteger(result, AgentMessage_HLOffset, headerLength); err != nil {
		return make([]byte, 1), err
	}

	startPosition := AgentMessage_MessageTypeOffset
	endPosition := AgentMessage_MessageTypeOffset + AgentMessage_MessageTypeLength - 1
	if err = putString(result, startPosition, endPosition, agentMessage.MessageType); err != nil {
		return make([]byte, 1), err
	}

	if err = putUInteger(result, AgentMessage_SchemaVersionOffset, agentMessage.SchemaVersion); err != nil {
		return make([]byte, 1), err
	}

	if err = putULong(result, AgentMessage_CreatedDateOffset, agentMessage.CreatedDate); err != nil {
		return make([]byte, 1), err
	}

	if err = putLong(result, AgentMessage_SequenceNumberOffset, agentMessage.SequenceNumber); err != nil {
		return make([]byte, 1), err
	}

	if err = putULong(result, AgentMessage_FlagsOffset, agentMessage.Flags); err != nil {
		return make([]byte, 1), err
	}

	if err = putUuid(result, AgentMessage_MessageIdOffset, agentMessage.MessageId); err != nil {
		return make([]byte, 1), err
	}

	hasher := sha256.New()
	hasher.Write(agentMessage.Payload)

	startPosition = AgentMessage_PayloadDigestOffset
	endPosition = AgentMessage_PayloadDigestOffset + AgentMessage_PayloadDigestLength - 1
	if err = putBytes(result, startPosition, endPosition, hasher.Sum(nil)); err != nil {
		return make([]byte, 1), err
	}

	if err = putUInteger(result, AgentMessage_PayloadTypeOffset, agentMessage.PayloadType); err != nil {
		return make([]byte, 1), err
	}

	if err = putUInteger(result, AgentMessage_PayloadLengthOffset, agentMessage.PayloadLength); err != nil {
		return make([]byte, 1), err
	}

	startPosition = AgentMessage_PayloadOffset
	endPosition = AgentMessage_PayloadOffset + int(payloadLength) - 1
	if err = putBytes(result, startPosition, endPosition, agentMessage.Payload); err != nil {
		return make([]byte, 1), err
	}

	return result, nil
}

// putUInteger puts an unsigned integer
func putUInteger(byteArray []byte, offset int, value uint32) (err error) {
	return putInteger(byteArray, offset, int32(value))
}

// putInteger puts an integer value to a byte array starting from the specified offset.
func putInteger(byteArray []byte, offset int, value int32) (err error) {
	byteArrayLength := len(byteArray)
	if offset > byteArrayLength-1 || offset+4 > byteArrayLength-1 || offset < 0 {
		return errors.New("Offset is outside the byte array.")
	}

	bytes, err := integerToBytes(value)
	if err != nil {
		return err
	}

	copy(byteArray[offset:offset+4], bytes)
	return nil
}

// integerToBytes gets bytes array from an integer.
func integerToBytes(input int32) (result []byte, err error) {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.BigEndian, input)
	if buf.Len() != 4 {
		return make([]byte, 4), errors.New("Input array size is not equal to 4.")
	}

	return buf.Bytes(), nil
}

// putString puts a string value to a byte array starting from the specified offset.
func putString(byteArray []byte, offsetStart int, offsetEnd int, inputString string) (err error) {
	byteArrayLength := len(byteArray)
	if offsetStart > byteArrayLength-1 || offsetEnd > byteArrayLength-1 || offsetStart > offsetEnd || offsetStart < 0 {
		return errors.New("Offset is outside the byte array.")
	}

	if offsetEnd-offsetStart+1 < len(inputString) {
		return errors.New("Not enough space to save the string.")
	}

	// wipe out the array location first and then insert the new value.
	for i := offsetStart; i <= offsetEnd; i++ {
		byteArray[i] = ' '
	}

	copy(byteArray[offsetStart:offsetEnd+1], inputString)
	return nil
}

// putULong puts an unsigned long integer.
func putULong(byteArray []byte, offset int, value uint64) (err error) {
	return putLong(byteArray, offset, int64(value))
}

// putLong puts a long integer value to a byte array starting from the specified offset.
func putLong(byteArray []byte, offset int, value int64) (err error) {
	byteArrayLength := len(byteArray)
	if offset > byteArrayLength-1 || offset+8 > byteArrayLength-1 || offset < 0 {
		return errors.New("Offset is outside the byte array.")
	}

	mbytes, err := longToBytes(value)
	if err != nil {
		return err
	}

	copy(byteArray[offset:offset+8], mbytes)
	return nil
}

// putUuid puts the 128 bit uuid to an array of bytes starting from the offset.
func putUuid(byteArray []byte, offset int, input uuid.UUID) (err error) {

	byteArrayLength := len(byteArray)
	if offset > byteArrayLength-1 || offset+16-1 > byteArrayLength-1 || offset < 0 {
		return errors.New("Offset is outside the byte array.")
	}

	b := []byte(input.String())
	leastSignificantLong, err := bytesToLong(b[8:16])
	if err != nil {
		return errors.New("Failed to get leastSignificant Long value.")
	}

	mostSignificantLong, err := bytesToLong(b[0:8])
	if err != nil {
		return errors.New("Failed to get mostSignificantLong Long value.")
	}

	if err = putLong(byteArray, offset, leastSignificantLong); err != nil {
		return errors.New("Failed to put leastSignificantLong Long value.")
	}

	if err = putLong(byteArray, offset+8, mostSignificantLong); err != nil {
		return errors.New("Failed to put mostSignificantLong Long value.")
	}

	return nil
}

// putBytes puts bytes into the array at the correct offset.
func putBytes(byteArray []byte, offsetStart int, offsetEnd int, inputBytes []byte) (err error) {
	byteArrayLength := len(byteArray)
	if offsetStart > byteArrayLength-1 || offsetEnd > byteArrayLength-1 || offsetStart > offsetEnd || offsetStart < 0 {
		return errors.New("Offset is outside the byte array.")
	}

	if offsetEnd-offsetStart+1 != len(inputBytes) {
		return errors.New("Not enough space to save the bytes.")
	}

	copy(byteArray[offsetStart:offsetEnd+1], inputBytes)
	return nil
}

// PrepareReplyPayloadToUpdateDocumentStatus creates the payload object for SendReply based on document status change.
func PrepareReplyPayloadToUpdateDocumentStatus(agentInfo AgentInfo, documentStatus ResultStatus, documentTraceOutput string, ableToOpenMGSConnection *bool) (payload SendReplyPayload) {
	payload = SendReplyPayload{
		AdditionalInfo: AdditionalInfo{
			Agent:                   agentInfo,
			DateTime:                toISO8601(uint64(time.Now().Unix())),
			AbleToOpenMGSConnection: ableToOpenMGSConnection,
		},
		DocumentStatus:      documentStatus,
		DocumentTraceOutput: documentTraceOutput,
		RuntimeStatus:       nil,
	}
	return
}

// AgentInfo represents the agent response
type AgentInfo struct {
	Lang      string `json:"lang"`
	Name      string `json:"name"`
	Os        string `json:"os"`
	OsVersion string `json:"osver"`
	Version   string `json:"ver"`
}

// SendReplyPayload represents the json structure of a reply sent to MDS.
type SendReplyPayload struct {
	AdditionalInfo      AdditionalInfo                  `json:"additionalInfo"`
	DocumentStatus      ResultStatus                    `json:"documentStatus"`
	DocumentTraceOutput string                          `json:"documentTraceOutput"`
	RuntimeStatus       map[string]*PluginRuntimeStatus `json:"runtimeStatus"`
}

// AdditionalInfo section in agent response
type AdditionalInfo struct {
	Agent                   AgentInfo      `json:"agent"`
	DateTime                string         `json:"dateTime"`
	RunID                   string         `json:"runId"`
	RuntimeStatusCounts     map[string]int `json:"runtimeStatusCounts"`
	AbleToOpenMGSConnection *bool          `json:"ableToOpenMGSConnection,omitempty"`
}

// PluginRuntimeStatus represents plugin runtime status section in agent response
type PluginRuntimeStatus struct {
	Status             ResultStatus `json:"status"`
	Code               int          `json:"code"`
	Name               string       `json:"name"`
	Output             string       `json:"output"`
	StartDateTime      string       `json:"startDateTime"`
	EndDateTime        string       `json:"endDateTime"`
	OutputS3BucketName string       `json:"outputS3BucketName"`
	OutputS3KeyPrefix  string       `json:"outputS3KeyPrefix"`
	StepName           string       `json:"stepName"`
	StandardOutput     string       `json:"standardOutput"`
	StandardError      string       `json:"standardError"`
}

func sendDocResponse(payloadDoc SendReplyPayload, docState *DocumentState, conn *websocket.Conn) {
	replyUUID := uuid.New()
	commandTopic := GetTopicFromDocResult(RunCommandResult, docState.DocumentType)
	agentMsg, err := GenerateAgentJobReplyPayload(replyUUID, docState.DocumentInformation.MessageID, payloadDoc, commandTopic)
	if err != nil {
		fmt.Printf("error while generating agent job reply payload: %v\n", err)
		return
	}
	msg, err := agentMsg.Serialize()
	if err != nil {
		// Should never happen
		fmt.Printf("error serializing agent message: %v\n", err)
		return
	}
	if err = conn.WriteMessage(websocket.BinaryMessage, msg); err == nil {
		fmt.Printf("successfully sent document response with client message id : %v for CommandId %s\n", replyUUID, docState.DocumentInformation.CommandID)
	} else {
		fmt.Printf("error while sending document response message with client message id : %v, err: %v\n", replyUUID, err)
	}
}

type ResultType string

const (
	RunCommandResult ResultType = "RunCommandResult"
	SessionResult    ResultType = "SessionResult"
)

// GetTopicFromDocResult returns topic based on doc result
func GetTopicFromDocResult(resultType ResultType, documentType DocumentType) CommandTopic {
	var commandTopic CommandTopic
	if resultType == RunCommandResult {
		if documentType == CancelCommand {
			commandTopic = CancelCommandTopic // we do not send replies using this topic mostly
		} else {
			commandTopic = SendCommandTopic // use send command as default
		}
	} else if resultType == SessionResult {
		panic("session?")
	}
	return commandTopic
}

// GenerateAgentJobReplyPayload generates AgentJobReply agent message
func GenerateAgentJobReplyPayload(agentMessageUUID uuid.UUID, messageID string, replyPayload SendReplyPayload, topic CommandTopic) (*AgentMessage, error) {
	payloadB, err := json.Marshal(replyPayload)
	if err != nil {
		fmt.Printf("could not marshal reply payload! %v\n", err)
		return nil, err
	}
	payload := string(payloadB)
	if len(payloadB) > ControlChannelAgentReplyPayloadSizeLimit {
		return nil, fmt.Errorf("dropping reply message %v because it is too large to send over control channel", agentMessageUUID.String())
	}
	finalReplyContent := AgentJobReplyContent{
		SchemaVersion: 1,
		JobId:         messageID,
		Content:       payload,
		Topic:         string(topic),
	}

	finalReplyContentBytes, err := json.Marshal(finalReplyContent)
	if err != nil {
		fmt.Printf("Cannot build reply message %v\n", err)
		return nil, err
	}

	repMsg := &AgentMessage{
		MessageType:    "agent_job_reply",
		SchemaVersion:  1,
		CreatedDate:    uint64(time.Now().UnixNano() / 1000000),
		SequenceNumber: 0,
		Flags:          0,
		MessageId:      agentMessageUUID,
		Payload:        finalReplyContentBytes,
	}
	return repMsg, nil
}

type CommandTopic string

const (
	// SendCommandTopic represents the topic added in the agent message payload for the document replies
	// for documents executed with topic aws.ssm.sendCommand
	SendCommandTopic CommandTopic = "aws.ssm.sendCommand"

	// CancelCommandTopic represents the topic added in the agent message payload for the document replies
	// for documents executed with topic aws.ssm.cancelCommand
	CancelCommandTopic CommandTopic = "aws.ssm.cancelCommand"

	// ControlChannelAgentReplyPayloadSizeLimit represents 120000 bytes is the maximum agent reply payload
	// in a message that can be sent through control channel.
	ControlChannelAgentReplyPayloadSizeLimit = 120000
)

// DocumentResult is a struct that stores information about the result of the document
type DocumentResult struct {
	DocumentName        string
	DocumentVersion     string
	MessageID           string
	AssociationID       string
	PluginResults       map[string]*PluginResult
	Status              ResultStatus
	LastPlugin          string
	NPlugins            int
	UpstreamServiceName UpstreamServiceName
	ResultType          ResultType
	RelatedDocumentType DocumentType
}

// constructMessage constructs agent message with the reply as payload
func constructMessage(result *DocumentResult) (*AgentMessage, error) {
	agentInfo := AgentInfo{
		Os:      "linux",
		Version: "3.3.0.1",
		Name:    "some-agent",
	}
	replyPayload := FormatPayload(result.LastPlugin, agentInfo, result.PluginResults)
	commandTopic := GetTopicFromDocResult(result.ResultType, result.RelatedDocumentType)
	return GenerateAgentJobReplyPayload(uuid.New(), result.MessageID, replyPayload, commandTopic)
}

// FormatPayload builds SendReply Payload from the internal plugins map
func FormatPayload(pluginID string, agentInfo AgentInfo, outputs map[string]*PluginResult) SendReplyPayload {
	status, statusCount, runtimeStatuses, _ := DocumentResultAggregator(pluginID, outputs)
	additionalInfo := AdditionalInfo{
		Agent:               agentInfo,
		DateTime:            toISO8601(uint64(time.Now().Unix())),
		RuntimeStatusCounts: statusCount,
	}
	payload := SendReplyPayload{
		AdditionalInfo:      additionalInfo,
		DocumentStatus:      status,
		DocumentTraceOutput: "", // TODO: Fill me appropriately
		RuntimeStatus:       runtimeStatuses,
	}
	return payload
}

// DocumentResultAggregator aggregates the result from the plugins to construct the agent response
func DocumentResultAggregator(
	pluginID string,
	pluginOutputs map[string]*PluginResult) (documentStatus ResultStatus, runtimeStatusCounts map[string]int,
	runtimeStatusesFiltered map[string]*PluginRuntimeStatus, runtimeStatuses map[string]*PluginRuntimeStatus) {

	runtimeStatuses = make(map[string]*PluginRuntimeStatus)
	for pluginID, pluginResult := range pluginOutputs {
		rs := prepareRuntimeStatus(*pluginResult)
		runtimeStatuses[pluginID] = &rs
	}
	// TODO instance this needs to be revised to be in parity with ec2config
	documentStatus = ResultStatusSuccess
	runtimeStatusCounts = map[string]int{}
	pluginCounts := len(runtimeStatuses)

	for _, pluginResult := range runtimeStatuses {
		runtimeStatusCounts[string(pluginResult.Status)]++
	}
	if pluginID == "" {
		//	  New precedence order of plugin states
		//	  Failed > TimedOut > Cancelled > Success > Cancelling > InProgress > Pending
		//	  The above order is a contract between SSM service and agent and hence for the calculation of aggregate
		//	  status of a (command) document, we follow the above precedence order.
		//
		//	  Note:
		//	  A command could have been failed/cancelled even before a plugin started executing, during which pendingItems > 0
		//	  but overallResult.Status would be Failed/Cancelled. That's the reason we check for OverallResult status along
		//	  with number of failed/cancelled items.
		//    TODO : We need to handle above to be able to send document traceoutput in case of document level errors.

		// Skipped is a form of success
		successCounts := runtimeStatusCounts[string(ResultStatusSuccess)] + runtimeStatusCounts[string(ResultStatusSkipped)]

		if runtimeStatusCounts[string(ResultStatusSuccessAndReboot)] > 0 {
			documentStatus = ResultStatusSuccessAndReboot
		} else if runtimeStatusCounts[string(ResultStatusFailed)] > 0 {
			documentStatus = ResultStatusFailed
		} else if runtimeStatusCounts[string(ResultStatusTimedOut)] > 0 {
			documentStatus = ResultStatusTimedOut
		} else if runtimeStatusCounts[string(ResultStatusCancelled)] > 0 {
			documentStatus = ResultStatusCancelled
		} else if successCounts == pluginCounts {
			documentStatus = ResultStatusSuccess
		} else {
			documentStatus = ResultStatusInProgress
		}
	} else {
		documentStatus = ResultStatusInProgress
	}

	runtimeStatusesFiltered = make(map[string]*PluginRuntimeStatus)

	// When pluginID isn't empty, the agent is sending update information about the current plugin.
	// When it's empty, the agent is sending update information about the document.
	// When it's empty, the agent only sends plugin status if it's a single plugin invocation.
	if pluginID != "" {
		runtimeStatusesFiltered[pluginID] = runtimeStatuses[pluginID]
		runtimeStatuses = runtimeStatusesFiltered
	} else if len(runtimeStatuses) <= 1 {
		runtimeStatusesFiltered = runtimeStatuses
	}

	return documentStatus, runtimeStatusCounts, runtimeStatusesFiltered, runtimeStatuses

}

// TODO move part of the function to service?
// prepareRuntimeStatus creates the structure for the runtimeStatus section of the payload of SendReply
// for a particular plugin.
func prepareRuntimeStatus(pluginResult PluginResult) PluginRuntimeStatus {
	var resultAsString string

	if pluginResult.Error == "" {
		resultAsString = fmt.Sprintf("%v", pluginResult.Output)
	} else {
		resultAsString = pluginResult.Error
	}

	runtimeStatus := PluginRuntimeStatus{
		Code:           pluginResult.Code,
		Name:           pluginResult.PluginName,
		Status:         pluginResult.Status,
		Output:         resultAsString,
		StartDateTime:  toISO8601(uint64(pluginResult.StartDateTime.Unix())),
		EndDateTime:    toISO8601(uint64(pluginResult.EndDateTime.Unix())),
		StandardOutput: pluginResult.StandardOutput,
		StandardError:  pluginResult.StandardError,
	}

	if pluginResult.OutputS3BucketName != "" {
		runtimeStatus.OutputS3BucketName = pluginResult.OutputS3BucketName
		if pluginResult.OutputS3KeyPrefix != "" {
			runtimeStatus.OutputS3KeyPrefix = pluginResult.OutputS3KeyPrefix
		}
		runtimeStatus.StepName = pluginResult.StepName
	}

	if runtimeStatus.Status == ResultStatusFailed && runtimeStatus.Code == 0 {
		runtimeStatus.Code = 1
	}

	return runtimeStatus
}
