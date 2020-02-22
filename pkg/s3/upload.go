package s3

import (
	"log"
	"os"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/loqutus/artifactory-replication/pkg/binary"
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
	fileSHA256, err := binary.ComputeFileSHA256(tempFileName)
	if err != nil {
		return err
	}
	log.Println("Uploading "+destinationFileName+" to "+destinationRegistry, "SHA256:", fileSHA256)
	_, err = uploader.Upload(&s3manager.UploadInput{
		Bucket: aws.String(destinationRegistry),
		Key:    aws.String(destinationFileName),
		Body:   f,
		Metadata: map[string]*string{
			"sha256": aws.String(fileSHA256),
		}})
	return err
}
