package s3

import (
	"log"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
)

func Delete(bucket string, files []string) ([]string, error) {
	var deleteFailed []string
	sess, err := session.NewSession(&aws.Config{})
	if err != nil {
		return nil, err
	}
	svc := s3.New(sess)
	for _, file := range files {
		log.Println("removing: ", file)
		input := &s3.DeleteObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(file),
		}
		backOffTime := backOffStart
		var failed bool
		for i := 1; i <= backOffSteps; i++ {
			_, err := svc.DeleteObject(input)
			if err != nil {
				failed = true
				log.Print("error s3 delete", file, "retry", string(i))
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
			deleteFailed = append(deleteFailed, file)
		}
	}
	return deleteFailed, nil
}
