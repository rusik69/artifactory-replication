package s3

import (
	"io/ioutil"
	"log"
	"strings"
)

func DownloadAllFiles(bucket string, objectPath string, allFiles []string) (string, error) {
	log.Println("downloading all files in", objectPath)
	var allFilesInDir []string
	for _, file := range allFiles {
		if strings.HasPrefix(file, objectPath) {
			allFilesInDir = append(allFilesInDir, file)
		}
	}
	tempDir, err := ioutil.TempDir("", "s3-download")
	if err != nil {
		return "", err
	}

	for _, file := range allFilesInDir {
		log.Println("Downloading", file)
		_, err := Download(bucket, file, tempDir)
		if err != nil {
			return "", err
		}
	}
	return tempDir, nil
}
