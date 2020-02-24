package oss

import (
	"log"
	"os"
	"time"

	"github.com/loqutus/aliyun-oss-go-sdk/oss"
	"github.com/loqutus/artifactory-replication/pkg/credentials"
)

func Upload(destinationRegistry string, fileName string, creds credentials.Creds, tempFileName string, endpoint string) error {
	log.Println("Uploading " + fileName + " to " + destinationRegistry)
	ossClient, err := oss.New(endpoint, creds.DestinationUser, creds.DestinationPassword)
	if err != nil {
		return err
	}
	bucket, err := ossClient.Bucket(destinationRegistry)
	if err != nil {
		return err
	}
	f, err := os.Open(tempFileName)
	if err != nil {
		return err
	}
	attempts := 5
	for i := 0; i < attempts; i++ {
		if i >= 1 {
			log.Println(err)
			log.Printf("Attempt: %d\n", i)
		}
		err = bucket.PutObject(fileName, f)
		if err == nil {
			break
		}
		time.Sleep(5 * time.Second)
	}
	return err
}
