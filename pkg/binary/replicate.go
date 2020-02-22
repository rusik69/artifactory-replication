package binary

import (
	"log"
	"os"
	"strings"
)

func Replicate(creds Creds, sourceRegistry string, destinationRegistry string, destinationRegistryType string, sourceRepo string, force string, helmCdnDomain string) []string {
	log.Println("Replicating repo " + sourceRegistry + "/" + sourceRepo + " to " + destinationRegistry + "/" + sourceRepo)
	var replicatedRealArtifacts []string
	sourceBinariesList, err := listArtifactoryFiles(sourceRegistry, sourceRepo, creds.SourceUser, creds.SourcePassword)
	if err != nil {
		err2 := sendSlackNotification(err.Error())
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
			destinationBinariesList, err = listS3Files(destinationRegistry)
			if err != nil {
				err2 := sendSlackNotification(err.Error())
				if err2 != nil {
					log.Println(err)
					panic(err2)
				}
				panic(err)
			}
			log.Println("Found destination binaries:", len(destinationBinariesList))
		}
	} else if destinationRegistryType == "artifactory" {
		destinationBinariesList, err = listArtifactoryFiles(destinationRegistry, sourceRepo, creds.DestinationUser, creds.DestinationPassword)
		if err != nil {
			err2 := sendSlackNotification(err.Error())
			if err2 != nil {
				log.Println(err)
				panic(err2)
			}
			panic(err)
		}
		log.Println("Found destination binaries:", len(destinationBinariesList))
	} else if destinationRegistryType == "oss" {
		destinationBinariesList, err = listOssFiles(destinationRegistry, creds, endpoint)
		if err != nil {
			err2 := sendSlackNotification(err.Error())
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
			replicatedRealArtifactsTemp := replicateBinary(creds, sourceRegistry, destinationRegistry, destinationRegistryType, sourceRepo+"/"+fileNameWithoutRepo, force, helmCdnDomain)
			for _, v := range replicatedRealArtifactsTemp {
				replicatedRealArtifacts = append(replicatedRealArtifacts, v)
			}
		} else {
			fileNameSplit := strings.Split(fileName, "/")
			fileNameWithoutPath := fileNameSplit[len(fileNameSplit)-1]
			fileURL := "http://" + sourceRegistry + "/artifactory/" + sourceRepo + "/" + fileNameWithoutPath
			fileFound := false
			for destinationFileName := range destinationBinariesList {
				if destinationFileName == fileName {
					log.Println("Found binary in destination: " + destinationFileName)
					fileFound = true
					break
				}
			}
			var doSync bool
			for _, st := range alwaysSyncList {
				if fileNameWithoutPath == st {
					doSync = true
					break
				}
			}
			if !fileFound || doSync || force == "true" {
				tempFileName, err := downloadFromArtifactory(fileURL, helmCdnDomain)
				if err != nil {
					log.Println("downloadFromArtifactory failed:")
					log.Println(err)
					failedArtifactoryDownload = append(failedArtifactoryDownload, fileURL)
					continue
				}
				repoWithoutPathSplit := strings.Split(sourceRepo, "/")
				repoWithoutPath := repoWithoutPathSplit[1]
				destinationFileName := repoWithoutPath + "/" + fileName
				destinationFileName = destinationFileName[strings.IndexByte(destinationFileName, '/'):]
				log.Println("Dest: " + destinationFileName)
				if destinationRegistryType == "s3" {
					err := uploadToS3(destinationRegistry, destinationFileName, tempFileName)
					if err != nil {
						log.Println("uploadToS3 failed:")
						log.Println(err)
						failedS3Upload = append(failedS3Upload, destinationFileName)
						continue
					}
				} else if destinationRegistryType == "artifactory" {
					err := uploadToArtifactory(destinationRegistry, sourceRepo, fileName, creds.DestinationUser, creds.DestinationPassword, tempFileName)
					if err != nil {
						panic(err)
					}
				} else if destinationRegistryType == "oss" {
					destinationFileName = strings.TrimPrefix(destinationFileName, "/")
					err := uploadToOss(destinationRegistry, destinationFileName, creds, tempFileName, endpoint)
					if err != nil {
						panic(err)
					}
				}
				if !doSync && !(force == "true") {
					fileNameSplit := strings.Split(fileName, "/")
					fileNameWithoutRepo := fileNameSplit[len(fileNameSplit)-1]
					replicatedRealArtifacts = append(replicatedRealArtifacts, sourceRepo+"/"+fileNameWithoutRepo)
				}
				os.Remove(tempFileName)
			}
		}
	}
	log.Printf("%d artifacts copied to %s\n", len(replicatedRealArtifacts), sourceRepo)
	return replicatedRealArtifacts
}
