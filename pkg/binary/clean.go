package binary

import (
	"errors"
	"log"
	"strconv"
	"time"

	"github.com/loqutus/artifactory-replication/pkg/artifactory"
	"github.com/loqutus/artifactory-replication/pkg/credentials"
	"github.com/loqutus/artifactory-replication/pkg/s3"
)

func removeStringFromSlice(slice []string, s int) []string {
	return append(slice[:s], slice[s+1:]...)
}

func Clean(destinationRegistry string, destinationRegistryType string, sourceRegistry string, artifactFilterProd string, creds credentials.Creds, keepDays int) ([]string, error) {
	log.Println("Cleaning repo " + destinationRegistry + " from files older than " + strconv.Itoa(keepDays) + " days and not in repo " + sourceRegistry + "/" + artifactFilterProd)
	var filesToRemove []string
	if destinationRegistryType != "s3" {
		return nil, errors.New("Unknown destination registry type: " + destinationRegistryType)
	}
	log.Println("artifactory.ListAllFiles " + sourceRegistry + "/" + artifactFilterProd)
	sourceFilesProd, err := artifactory.ListAllFiles(sourceRegistry, artifactFilterProd, creds.SourceUser, creds.SourcePassword)
	if err != nil {
		return nil, err
	}
	log.Println("got " + string(strconv.Itoa(len(sourceFilesProd))) + " files from artifactory repo " + artifactFilterProd)
	log.Println("s3.GetFilesModificationDate: " + destinationRegistry)
	destinationFiles, err := s3.GetFilesModificationDate(destinationRegistry)
	if err != nil {
		return nil, err
	}
	log.Println("got " + string(strconv.Itoa(len(destinationFiles))) + " files with modification date from " + destinationRegistry)
	var excludedCounter int
	for _, sourceFile := range sourceFilesProd {
		for destinationFile, _ := range destinationFiles {
			//log.Println(sourceFile, destinationFile)
			if sourceFile == destinationFile {
				excludedCounter++
				//log.Println("exluding " + sourceFile + " from removal")
				delete(destinationFiles, destinationFile)
			}
		}
	}
	log.Println("Excluded " + strconv.Itoa(excludedCounter) + " from removal")
	timeKeep := time.Now().Add(time.Duration(-keepDays) * time.Hour)
	for fileName, modificationDate := range destinationFiles {
		if modificationDate.Before(timeKeep) {
			filesToRemove = append(filesToRemove, fileName)
		}
	}
	/* for _, file := range filesToRemove {
		log.Println(file)
	} */
	log.Println("removing " + strconv.Itoa(len(filesToRemove)) + " files from " + destinationRegistry)
	/* removeFailed, err := s3.Delete(destinationRegistry, filesToRemove)
	if err != nil {
		return nil, err
	}
	if len(removeFailed) > 0 {
		log.Println("error removing files:")
		for _, file := range removeFailed {
			log.Println(file)
		}
	} */
	/* if len(filesToRemove) > 0 {
		err := helm.RegenerateIndexYaml(replicatedRealArtifacts, replicatedRealArtifactsProd, sourceRegistry, destinationRegistry, repoName, repoNameProd, helmCdnDomain)
		if err != nil {
			log.Println("error regenerating index.yaml")
			panic(err)
		}
	} */
	return filesToRemove, nil
}