package oss

import (
	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"github.com/loqutus/artifactory-replication/pkg/credentials"
)

func ListFiles(sourceRepo string, creds credentials.Creds, endpoint string) (map[string]bool, error) {
	output := make(map[string]bool)
	ossClient, err := oss.New(endpoint, creds.DestinationUser, creds.DestinationPassword)
	if err != nil {
		return output, err
	}
	bucket, err := ossClient.Bucket(sourceRepo)
	if err != nil {
		return output, err
	}
	lsRes, err := bucket.ListObjects()
	if err != nil {
		return output, err
	}
	for _, object := range lsRes.Objects {
		output[object.Key] = false
	}
	return output, nil
}
