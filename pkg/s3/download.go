package s3

import (
	"log"
	"os"
	"strings"

	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
)

func Download(bucket string, objectPath string, dir string) (string, error) {
	sess := session.Must(session.NewSession())
	downloader := s3manager.NewDownloader(sess)
	s := strings.Split(objectPath, "/")
	fileName := s[len(s)-1]
	f, err := os.Create(dir + string('/') + fileName)
	if err != nil {
		return "", err
	}
	var failed bool
	backOffTime := backOffStart
	defer f.Close()
	for i := 1; i <= backOffSteps; i++ {
		_, err := downloader.Download(f, &s3.GetObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(objectPath),
		})
		if err != nil {
			failed = true
			log.Println("error downloader.Download:", err.Error())
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
	return fileName, nil
}
