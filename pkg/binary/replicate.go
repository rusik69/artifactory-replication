package binary

import (
	"log"
	"os"
	"regexp"
	"strings"

	"github.com/loqutus/artifactory-replication/pkg/artifactory"
	"github.com/loqutus/artifactory-replication/pkg/credentials"
	"github.com/loqutus/artifactory-replication/pkg/oss"
	"github.com/loqutus/artifactory-replication/pkg/s3"
	"github.com/loqutus/artifactory-replication/pkg/slack"
)

func Replicate(creds credentials.Creds, sourceRegistry string, destinationRegistry string, destinationRegistryType string, sourceRepo string, force string, helmCdnDomain string, syncPattern string) ([]string, []string) {
	log.Println("Replicating repo " + sourceRegistry + "/" + sourceRepo + " to " + destinationRegistry + "/" + sourceRepo)
	sourceBinariesList, err := artifactory.ListFiles(sourceRegistry, sourceRepo, creds.SourceUser, creds.SourcePassword)
	var replicatedRealArtifacts, replicatedForcedArtifacts []string
	if err != nil {
		err2 := slack.SendMessage(err.Error())
		if err2 != nil {
			log.Println(err)
			panic(err2)
		}
		panic(err)
	}
	log.Println("Found source binaries:", len(sourceBinariesList))
	endpoint := os.Getenv("OSS_ENDPOINT")
	if endpoint == "" {
		endpoint = "oss-cn-beijing.aliyuncs.com"
	}
	if destinationRegistryType == "s3" {
		if len(destinationBinariesList) == 0 {
			destinationBinariesList, err = s3.ListFiles(destinationRegistry)
			if err != nil {
				err2 := slack.SendMessage(err.Error())
				if err2 != nil {
					log.Println(err)
					panic(err2)
				}
				panic(err)
			}
			log.Println("Found destination binaries:", len(destinationBinariesList))
		}
	} else if destinationRegistryType == "artifactory" {
		destinationBinariesList, err = artifactory.ListFiles(destinationRegistry, sourceRepo, creds.DestinationUser, creds.DestinationPassword)
		if err != nil {
			err2 := slack.SendMessage(err.Error())
			if err2 != nil {
				log.Println(err)
				panic(err2)
			}
			panic(err)
		}
		log.Println("Found destination binaries:", len(destinationBinariesList))
	} else if destinationRegistryType == "oss" {
		destinationBinariesList, err = oss.ListFiles(destinationRegistry, creds, endpoint)
		if err != nil {
			err2 := slack.SendMessage(err.Error())
			if err2 != nil {
				log.Println(err)
				panic(err2)
			}
			panic(err)
		}
		log.Println("Found destination binaries:", len(destinationBinariesList))
	}

	for fileName, fileIsDir := range sourceBinariesList {
		if fileIsDir {
			log.Println("Processing source dir: " + fileName)
			fileNameSplit := strings.Split(fileName, "/")
			fileNameWithoutRepo := fileNameSplit[len(fileNameSplit)-1]
			replicatedRealArtifactsTemp, replicatedForcedArtifactsTemp := Replicate(creds, sourceRegistry, destinationRegistry, destinationRegistryType, sourceRepo+"/"+fileNameWithoutRepo, force, helmCdnDomain, syncPattern)
			for _, v := range replicatedRealArtifactsTemp {
				replicatedRealArtifacts = append(replicatedRealArtifacts, v)
			}
			for _, v := range replicatedForcedArtifactsTemp {
				replicatedForcedArtifacts = append(replicatedForcedArtifacts, v)
			}
		} else {
			fileNameSplit := strings.Split(fileName, "/")
			fileNameWithoutPath := fileNameSplit[len(fileNameSplit)-1]
			fileURL := "http://" + sourceRegistry + "/artifactory/" + sourceRepo + "/" + fileNameWithoutPath
			fileFound := false
			for destinationFileName := range destinationBinariesList {
				if destinationFileName == fileName {
					//log.Println("Found binary in destination: " + destinationFileName)
					fileFound = true
					break
				}
			}
			var doSync bool
			if syncPattern != "" {
				match, _ := regexp.MatchString(syncPattern, fileName)
				if match {
					doSync = true
					log.Println("Filename", fileName, "matched pattern", syncPattern)
				}

			}
			for _, st := range AlwaysSyncList {
				if fileNameWithoutPath == st {
					doSync = true
					break
				}
			}
			if !fileFound || doSync || force == "true" {
				tempFileName, err := artifactory.Download(fileURL, helmCdnDomain)
				if err != nil {
					log.Println("artifactory.Download failed:")
					log.Println(err)
					FailedArtifactoryDownload = append(FailedArtifactoryDownload, fileURL)
					continue
				}
				repoWithoutPathSplit := strings.Split(sourceRepo, "/")
				repoWithoutPath := repoWithoutPathSplit[1]
				destinationFileName := repoWithoutPath + "/" + fileName
				destinationFileName = destinationFileName[strings.IndexByte(destinationFileName, '/'):]
				log.Println("Dest: " + destinationFileName)
				if destinationRegistryType == "s3" {
					err := s3.Upload(destinationRegistry, destinationFileName, tempFileName)
					if err != nil {
						log.Println("s3.Upload failed:")
						log.Println(err)
						FailedS3Upload = append(FailedS3Upload, destinationFileName)
						continue
					}
				} else if destinationRegistryType == "artifactory" {
					err := artifactory.Upload(destinationRegistry, sourceRepo, fileName, creds.DestinationUser, creds.DestinationPassword, tempFileName)
					if err != nil {
						panic(err)
					}
				} else if destinationRegistryType == "oss" {
					destinationFileName = strings.TrimPrefix(destinationFileName, "/")
					err := oss.Upload(destinationRegistry, destinationFileName, creds, tempFileName, endpoint)
					if err != nil {
						panic(err)
					}
				}
				fileNameSplit := strings.Split(fileName, "/")
				fileNameWithoutRepo := fileNameSplit[len(fileNameSplit)-1]
				if !doSync && !(force == "true") {
					replicatedRealArtifacts = append(replicatedRealArtifacts, sourceRepo+"/"+fileNameWithoutRepo)
				} else {
					replicatedForcedArtifacts = append(replicatedForcedArtifacts, sourceRepo+"/"+fileNameWithoutRepo)
				}
				os.Remove(tempFileName)
			}
		}
	}
	return replicatedRealArtifacts, replicatedForcedArtifacts
}
