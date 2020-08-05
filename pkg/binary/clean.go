package binary

import (
	"errors"
	"log"
	"time"

	"github.com/loqutus/artifactory-replication/pkg/artifactory"
	"github.com/loqutus/artifactory-replication/pkg/credentials"
	"github.com/loqutus/artifactory-replication/pkg/s3"
)

func removeStringFromSlice(slice []string, s int) []string {
	return append(slice[:s], slice[s+1:]...)
}

func Clean(destinationRegistry string, destinationRegistryType string, sourceRegistry string, artifactFilter string, artifactFilterProd string, creds credentials.Creds, keepDays int) ([]string, error) {
	var filesToRemove []string
	if destinationRegistryType != "s3" {
		return nil, errors.New("Unknown destination registry type: " + destinationRegistryType)
	}
	destinationFiles, err := s3.ListFiles(destinationRegistry)
	if err != nil {
		return nil, err
	}
	sourceFilesProd, err := artifactory.ListFiles(sourceRegistry, artifactFilterProd, creds.SourceUser, creds.SourcePassword)
	if err != nil {
		return nil, err
	}
	for sourceFile, _ := range sourceFilesProd {
		for destinationFile, _ := range destinationFiles {
			if sourceFile == destinationFile {
				delete(destinationFiles, destinationFile)
			}
		}
	}
	var filesList []string
	for file, _ := range destinationFiles {
		filesList = append(filesList, file)
	}
	files, err := s3.GetFilesModificationDate(destinationRegistry, filesList)
	if err != nil {
		return nil, err
	}
	timeKeep := time.Now().Add(time.Duration(-keepDays) * time.Hour)
	for fileName, modificationDate := range files {
		if modificationDate.Before(timeKeep) {
			filesToRemove = append(filesToRemove, fileName)
		}
	}
	log.Println(filesToRemove)
	removeFailed, err := s3.Delete(destinationRegistry, filesToRemove)
	if err != nil {
		return nil, err
	}
	if len(removeFailed) > 0 {
		log.Println("error removing files:")
		for _, file := range removeFailed {
			log.Println(file)
		}
	}
	return filesToRemove, nil
}
