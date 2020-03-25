package s3

import (
	"log"
	"os"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/loqutus/artifactory-replication/pkg/sha256"
)

func Upload(destinationRegistry string, destinationFileName string, tempFileName string) error {
	f, err := os.Open(tempFileName)
	if err != nil {
		return err
	}
	sess, err := session.NewSession(&aws.Config{})
	if err != nil {
		return err
	}
	uploader := s3manager.NewUploader(sess, func(u *s3manager.Uploader) {
		u.PartSize = 5 * 1024 * 1024 // The minimum/default allowed part size is 5MB
		u.Concurrency = 2            // default is 5
	})
	fileSHA256, err := sha256.ComputeFileSHA256(tempFileName)
	if err != nil {
		return err
	}
	log.Println("Uploading "+destinationFileName+" to "+destinationRegistry, "SHA256:", fileSHA256)
	backOffTime := backOffStart
	var failed bool
	for i := 1; i <= backOffSteps; i++ {
		_, err = uploader.Upload(&s3manager.UploadInput{
			Bucket: aws.String(destinationRegistry),
			Key:    aws.String(destinationFileName),
			Body:   f,
			Metadata: map[string]*string{
				"sha256": aws.String(fileSHA256),
			}})
		if err != nil {
			failed = true
			log.Print("error s3 upload", destinationFileName, "retry", string(i))
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
		return err
	} else {
		return nil
	}
}
