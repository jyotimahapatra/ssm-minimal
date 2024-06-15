package main

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"
)

// QueryMetadata attempts to retrieve metadata from the AWS SDK with built-in retries.
func QueryMetadata(path string) (string, error) {
	sess, err := session.NewSession()
	if err != nil {
		return "", err
	}
	imds := ec2metadata.New(sess, aws.NewConfig().WithMaxRetries(5))
	return imds.GetMetadata(path)
}
