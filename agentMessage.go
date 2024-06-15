package main

import (
	"github.com/google/uuid"
)

// AgentMessage represents a message for agent to send/receive. AgentMessage Message in MGS is equivalent to MDS' InstanceMessage.
// All agent messages are sent in this form to the MGS service.
type AgentMessage struct {
	HeaderLength   uint32
	MessageType    string
	SchemaVersion  uint32
	CreatedDate    uint64
	SequenceNumber int64
	Flags          uint64
	MessageId      uuid.UUID
	PayloadDigest  []byte
	PayloadType    uint32
	PayloadLength  uint32
	Payload        []byte
}

// HL - HeaderLength is a 4 byte integer that represents the header length.
// MessageType is a 32 byte UTF-8 string containing the message type.
// SchemaVersion is a 4 byte integer containing the message schema version number.
// CreatedDate is an 8 byte integer containing the message create epoch millis in UTC.
// SequenceNumber is an 8 byte integer containing the message sequence number for serialized message streams.
// Flags is an 8 byte unsigned integer containing a packed array of control flags:
//   Bit 0 is SYN - SYN is set (1) when the recipient should consider Seq to be the first message number in the stream
//   Bit 1 is FIN - FIN is set (1) when this message is the final message in the sequence.
// MessageId is a 40 byte UTF-8 string containing a random UUID identifying this message.
// Payload digest is a 32 byte containing the SHA-256 hash of the payload.
// Payload Type is a 4 byte integer containing the payload type.
// Payload length is an 4 byte unsigned integer containing the byte length of data in the Payload field.
// Payload is a variable length byte data.
//
// | HL|         MessageType           |Ver|  CD   |  Seq  | Flags |
// |         MessageId                     |           Digest              |PayType| PayLen|
// |         Payload      			|

const (
	AgentMessage_HLLength             = 4
	AgentMessage_MessageTypeLength    = 32
	AgentMessage_SchemaVersionLength  = 4
	AgentMessage_CreatedDateLength    = 8
	AgentMessage_SequenceNumberLength = 8
	AgentMessage_FlagsLength          = 8
	AgentMessage_MessageIdLength      = 16
	AgentMessage_PayloadDigestLength  = 32
	AgentMessage_PayloadTypeLength    = 4
	AgentMessage_PayloadLengthLength  = 4
)

const (
	AgentMessage_HLOffset             = 0
	AgentMessage_MessageTypeOffset    = AgentMessage_HLOffset + AgentMessage_HLLength
	AgentMessage_SchemaVersionOffset  = AgentMessage_MessageTypeOffset + AgentMessage_MessageTypeLength
	AgentMessage_CreatedDateOffset    = AgentMessage_SchemaVersionOffset + AgentMessage_SchemaVersionLength
	AgentMessage_SequenceNumberOffset = AgentMessage_CreatedDateOffset + AgentMessage_CreatedDateLength
	AgentMessage_FlagsOffset          = AgentMessage_SequenceNumberOffset + AgentMessage_SequenceNumberLength
	AgentMessage_MessageIdOffset      = AgentMessage_FlagsOffset + AgentMessage_FlagsLength
	AgentMessage_PayloadDigestOffset  = AgentMessage_MessageIdOffset + AgentMessage_MessageIdLength
	AgentMessage_PayloadTypeOffset    = AgentMessage_PayloadDigestOffset + AgentMessage_PayloadDigestLength
	AgentMessage_PayloadLengthOffset  = AgentMessage_PayloadTypeOffset + AgentMessage_PayloadTypeLength
	AgentMessage_PayloadOffset        = AgentMessage_PayloadLengthOffset + AgentMessage_PayloadLengthLength
)

// Deserialize deserializes the byte array into an AgentMessage message.
// * Payload is a variable length byte data.
// * | HL|         MessageType           |Ver|  CD   |  Seq  | Flags |
// * |         MessageId                     |           Digest              |PayType| PayLen|
// * |         Payload      			|
func (agentMessage *AgentMessage) Deserialize(input []byte) (err error) {
	agentMessage.MessageType, err = getString(input, AgentMessage_MessageTypeOffset, AgentMessage_MessageTypeLength)
	if err != nil {
		//log.Errorf("Could not deserialize field MessageType with error: %v", err)
		return err
	}
	agentMessage.SchemaVersion, err = getUInteger(input, AgentMessage_SchemaVersionOffset)
	if err != nil {
		//log.Errorf("Could not deserialize field SchemaVersion with error: %v", err)
		return err
	}
	agentMessage.CreatedDate, err = getULong(input, AgentMessage_CreatedDateOffset)
	if err != nil {
		//log.Errorf("Could not deserialize field CreatedDate with error: %v", err)
		return err
	}
	agentMessage.SequenceNumber, err = getLong(input, AgentMessage_SequenceNumberOffset)
	if err != nil {
		//log.Errorf("Could not deserialize field SequenceNumber with error: %v", err)
		return err
	}
	agentMessage.Flags, err = getULong(input, AgentMessage_FlagsOffset)
	if err != nil {
		//log.Errorf("Could not deserialize field Flags with error: %v", err)
		return err
	}
	agentMessage.MessageId, err = getUuid(input, AgentMessage_MessageIdOffset)
	if err != nil {
		//log.Errorf("Could not deserialize field MessageId with error: %v", err)
		return err
	}

	agentMessage.PayloadDigest, err = getBytes(input, AgentMessage_PayloadDigestOffset, AgentMessage_PayloadDigestLength)
	if err != nil {
		//log.Errorf("Could not deserialize field PayloadDigest with error: %v", err)
		return err
	}
	agentMessage.PayloadType, err = getUInteger(input, AgentMessage_PayloadTypeOffset)
	if err != nil {
		//log.Errorf("Could not deserialize field PayloadType with error: %v", err)
		return err
	}

	agentMessage.PayloadLength, err = getUInteger(input, AgentMessage_PayloadLengthOffset)

	headerLength, herr := getUInteger(input, AgentMessage_HLOffset)
	if herr != nil {
		//log.Errorf("Could not deserialize field HeaderLength with error: %v", err)
		return err
	}

	agentMessage.HeaderLength = headerLength
	agentMessage.Payload = input[headerLength+AgentMessage_PayloadLengthLength:]

	return nil
}
