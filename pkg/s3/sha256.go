package s3

import (
	"errors"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
)

func GetSHA256(S3Bucket string, filename string) (string, error) {
	sess, _ := session.NewSession(&aws.Config{})
	svc := s3.New(sess)
	input := &s3.HeadObjectInput{
		Bucket: aws.String(S3Bucket),
		Key:    aws.String(filename),
	}
	object, err := svc.HeadObject(input)
	if err != nil {
		return "", err
	}
	if val, ok := object.Metadata["sha256"]; ok {
		return *val, nil
	}
	return "", errors.New("Missing sha256 in metadata")
}
