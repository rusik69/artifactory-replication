package binary

import (
	"log"
	"strings"

	"github.com/loqutus/artifactory-replication/pkg/artifactory"
	"github.com/loqutus/artifactory-replication/pkg/s3"
)

var destinationBinariesList map[string]bool

func checkBinaryRepos(sourceRegistry string, destinationRegistry string, destinationRegistryType string, creds Creds, dir string) error {
	log.Println("Getting source repos from: " + sourceRegistry)
	sourceFilesWithDirs, err := artifactory.ListFiles(sourceRegistry, dir, creds.SourceUser, creds.SourcePassword)
	if err != nil {
		log.Println("artifactory.ListFiles failed")
		return err
	}
	if len(destinationBinariesList) == 0 {
		destinationBinariesList, err = s3.ListFiles(destinationRegistry)
		if err != nil {
			log.Println("s3.ListFiles failed")
			return err
		}
	}
	for sourceFile, isDir := range sourceFilesWithDirs {
		if isDir {
			log.Println("Processing source dir: " + sourceFile)
			fileNameSplit := strings.Split(sourceFile, "/")
			fileNameWithoutRepo := fileNameSplit[len(fileNameSplit)-1]
			checkBinaryRepos(sourceRegistry, destinationRegistry, destinationRegistryType, creds, dir+"/"+fileNameWithoutRepo)
		} else {
			var found bool
			for destinationBinary := range destinationBinariesList {
				if destinationBinary == sourceFile {
					log.Println("Found:", destinationBinary)
					found = true
					sourceSHA256, err := getArtifactoryFileSHA256(sourceRegistry, sourceFile, creds.SourceUser, creds.SourcePassword)
					if err != nil {
						log.Println("Error getting source file sha256:", sourceFile)
						log.Println(err)
						checkFailed = true
						checkFailedList = append(checkFailedList, sourceFile)
						continue
					}
					destinationSHA256, err := getS3FileSHA256(destinationRegistry, destinationBinary)
					if err != nil {
						log.Println("Error getting destination file sha256:", destinationBinary)
						log.Println(err)
						checkFailed = true
						checkFailedList = append(checkFailedList, destinationBinary)
						continue
					}
					if sourceSHA256 != destinationSHA256 {
						log.Println("SHA256 mismatch:", destinationBinary)
						log.Println("Source SHA256:", sourceSHA256)
						log.Println("Destination SHA256:", destinationSHA256)
						checkFailed = true
						checkFailedList = append(checkFailedList, destinationBinary)
						continue
					}
					break
				}
			}
			if !found {
				checkFailed = true
				log.Println("Not found:", sourceFile)
				checkFailedList = append(checkFailedList, sourceFile)
			}
		}
	}
	return nil
}
