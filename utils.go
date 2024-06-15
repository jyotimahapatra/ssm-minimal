package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strings"

	"github.com/google/uuid"
)

// getString get a string value from the byte array starting from the specified offset to the defined length.
func getString(byteArray []byte, offset int, stringLength int) (result string, err error) {
	byteArrayLength := len(byteArray)
	if offset > byteArrayLength-1 || offset+stringLength-1 > byteArrayLength-1 || offset < 0 {
		//log.Error("getString failed: Offset is invalid.")
		return "", errors.New("Offset is outside the byte array.")
	}

	//remove nulls from the bytes array
	b := bytes.Trim(byteArray[offset:offset+stringLength], "\x00")

	return strings.TrimSpace(string(b)), nil
}

// getUInteger gets an unsigned integer
func getUInteger(byteArray []byte, offset int) (result uint32, err error) {
	var temp int32
	temp, err = getInteger(byteArray, offset)
	return uint32(temp), err
}

// getInteger gets an integer value from a byte array starting from the specified offset.
func getInteger(byteArray []byte, offset int) (result int32, err error) {
	byteArrayLength := len(byteArray)
	if offset > byteArrayLength-1 || offset+4 > byteArrayLength-1 || offset < 0 {
		//log.Error("getInteger failed: Offset is invalid.")
		return 0, errors.New("Offset is bigger than the byte array.")
	}
	return bytesToInteger(byteArray[offset : offset+4])
}

// bytesToInteger gets an integer from a byte array.
func bytesToInteger(input []byte) (result int32, err error) {
	var res int32
	inputLength := len(input)
	if inputLength != 4 {
		//log.Error("bytesToInteger failed: input array size is not equal to 4.")
		return 0, errors.New("Input array size is not equal to 4.")
	}
	buf := bytes.NewBuffer(input)
	binary.Read(buf, binary.BigEndian, &res)
	return res, nil
}

// getBytes gets an array of bytes starting from the offset.
func getBytes(byteArray []byte, offset int, byteLength int) (result []byte, err error) {
	byteArrayLength := len(byteArray)
	if offset > byteArrayLength-1 || offset+byteLength-1 > byteArrayLength-1 || offset < 0 {
		//log.Error("getBytes failed: Offset is invalid.")
		return make([]byte, byteLength), errors.New("Offset is outside the byte array.")
	}
	return byteArray[offset : offset+byteLength], nil
}

// getUuid gets the 128bit uuid from an array of bytes starting from the offset.
func getUuid(byteArray []byte, offset int) (result uuid.UUID, err error) {
	byteArrayLength := len(byteArray)
	if offset > byteArrayLength-1 || offset+16-1 > byteArrayLength-1 || offset < 0 {
		//log.Error("getUuid failed: Offset is invalid.")
		return uuid.UUID{}, errors.New("Offset is outside the byte array.")
	}

	leastSignificantLong, err := getLong(byteArray, offset)
	if err != nil {
		//log.Error("getUuid failed: failed to get uuid LSBs Long value.")
		return uuid.UUID{}, errors.New("Failed to get uuid LSBs long value.")
	}

	leastSignificantBytes, err := longToBytes(leastSignificantLong)
	if err != nil {
		//log.Error("getUuid failed: failed to get uuid LSBs bytes value.")
		return uuid.UUID{}, errors.New("Failed to get uuid LSBs bytes value.")
	}

	mostSignificantLong, err := getLong(byteArray, offset+8)
	if err != nil {
		//log.Error("getUuid failed: failed to get uuid MSBs Long value.")
		return uuid.UUID{}, errors.New("Failed to get uuid MSBs long value.")
	}

	mostSignificantBytes, err := longToBytes(mostSignificantLong)
	if err != nil {
		//log.Error("getUuid failed: failed to get uuid MSBs bytes value.")
		return uuid.UUID{}, errors.New("Failed to get uuid MSBs bytes value.")
	}

	uuidBytes := append(mostSignificantBytes, leastSignificantBytes...)
	u, _ := uuid.FromBytes(uuidBytes)

	return u, nil
}

// getLong gets a long integer value from a byte array starting from the specified offset. 64 bit.
func getLong(byteArray []byte, offset int) (result int64, err error) {
	byteArrayLength := len(byteArray)
	if offset > byteArrayLength-1 || offset+8 > byteArrayLength-1 || offset < 0 {
		//log.Error("getLong failed: Offset is invalid.")
		return 0, errors.New("Offset is outside the byte array.")
	}
	return bytesToLong(byteArray[offset : offset+8])
}

// bytesToLong gets a Long integer from a byte array.
func bytesToLong(input []byte) (result int64, err error) {
	var res int64
	inputLength := len(input)
	if inputLength != 8 {
		//log.Error("bytesToLong failed: input array size is not equal to 8.")
		return 0, errors.New("Input array size is not equal to 8.")
	}
	buf := bytes.NewBuffer(input)
	binary.Read(buf, binary.BigEndian, &res)
	return res, nil
}

// longToBytes gets bytes array from a long integer.
func longToBytes(input int64) (result []byte, err error) {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.BigEndian, input)
	if buf.Len() != 8 {
		//log.Error("longToBytes failed: buffer output length is not equal to 8.")
		return make([]byte, 8), errors.New("Input array size is not equal to 8.")
	}

	return buf.Bytes(), nil
}

// getULong gets an unsigned long integer
func getULong(byteArray []byte, offset int) (result uint64, err error) {
	var temp int64
	temp, err = getLong(byteArray, offset)
	return uint64(temp), err
}
