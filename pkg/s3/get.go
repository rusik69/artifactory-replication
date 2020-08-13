package s3

import (
	"log"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
)

func GetFilesModificationDate(S3Bucket string) (map[string]*time.Time, error) {
	sess, _ := session.NewSession(&aws.Config{})
	svc := s3.New(sess)
	var objects []s3.Object
	output := make(map[string]*time.Time)
	var err error
	var failed bool
	backOffTime := backOffStart
	for i := 1; i <= backOffSteps; i++ {
		err := svc.ListObjectsPages(&s3.ListObjectsInput{Bucket: &S3Bucket},
			func(p *s3.ListObjectsOutput, last bool) (shouldContinue bool) {
				for _, obj := range p.Contents {
					objects = append(objects, *obj)
				}
				return true
			})
		if err != nil {
			failed = true
			log.Print("error s3 list objects in bucket ", S3Bucket, " retry ", strconv.Itoa(i))
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
		return nil, err
	}
	for _, object := range objects {
		output[*object.Key] = object.LastModified
	}
	return output, nil
}
