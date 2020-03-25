package s3

import (
	"errors"
	"log"
	"time"

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
	var err error
	var failed bool
	backOffTime := backOffStart
	var object *s3.HeadObjectOutput
	for i := 1; i <= backOffSteps; i++ {
		object, err = svc.HeadObject(input)
		if err != nil {
			failed = true
			log.Print("error s3 HeadObject", S3Bucket, filename, "retry", string(i))
			if i != backOffSteps {
				time.Sleep(time.Duration(backOffTime) * time.Millisecond)
			}
			backOffTime *= i
		} else {
			failed = false
			break
		}
	}
	if failed == true {
		return "", err
	}
	if val, ok := object.Metadata["sha256"]; ok {
		return *val, nil
	}
	return "", errors.New("Missing sha256 in metadata")
}
