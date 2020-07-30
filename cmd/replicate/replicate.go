package main

import (
	"log"
	"os"
	"strings"

	"github.com/loqutus/artifactory-replication/pkg/binary"
	"github.com/loqutus/artifactory-replication/pkg/credentials"
	"github.com/loqutus/artifactory-replication/pkg/docker"
	"github.com/loqutus/artifactory-replication/pkg/ecr"
	"github.com/loqutus/artifactory-replication/pkg/helm"
	"github.com/loqutus/artifactory-replication/pkg/repos"
	"github.com/loqutus/artifactory-replication/pkg/slack"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	sourceRegistry := os.Getenv("SOURCE_REGISTRY")
	if sourceRegistry == "" {
		panic("empty SOURCE_REGISTRY env variable")
	}
	destinationRegistry := os.Getenv("DESTINATION_REGISTRY")
	if destinationRegistry == "" {
		panic("empty DESTINATION_REGISTRY env variable")
	}
	artifactFilter := os.Getenv("ARTIFACT_FILTER")
	artifactFilterProd := os.Getenv("ARTIFACT_FILTER_PROD")
	artifactType := os.Getenv("ARTIFACT_TYPE")
	destinationRegistryType := os.Getenv("DESTINATION_REGISTRY_TYPE")
	force := os.Getenv("FORCE")
	creds := credentials.Creds{
		SourceUser:          os.Getenv("SOURCE_USER"),
		SourcePassword:      os.Getenv("SOURCE_PASSWORD"),
		DestinationUser:     os.Getenv("DESTINATION_USER"),
		DestinationPassword: os.Getenv("DESTINATION_PASSWORD"),
	}
	checkReposFlag := os.Getenv("CHECK_REPOS")
	if checkReposFlag == "true" {
		if artifactType == "docker" || artifactType == "binary" {
			repos.Check(sourceRegistry, destinationRegistry, creds, artifactType, destinationRegistryType, artifactFilter)
		} else {
			log.Println("unknown artifact type: ", artifactType)
			os.Exit(1)
		}
		os.Exit(0)
	}
	if artifactType == "docker" {
		if artifactFilter != "" {
			log.Println("Replicating docker images repo " + artifactFilter + " from " + sourceRegistry + " to " + destinationRegistry)
		} else {
			log.Println("Replicating docker images from " + sourceRegistry + " to " + destinationRegistry)
		}
		if destinationRegistryType == "aws" {
			currentAccessKey := os.Getenv("AWS_ACCESS_KEY_ID")
			if currentAccessKey == "" {
				os.Setenv("AWS_ACCESS_KEY_ID", creds.DestinationUser)
			}
			currentSecretKey := os.Getenv("AWS_SECRET_ACCESS_KEY")
			if currentSecretKey == "" {
				os.Setenv("AWS_SECRET_ACCESS_KEY", creds.DestinationPassword)
			}
			ECRLogin, ECRPassword, err := ecr.GetToken()
			if err != nil {
				panic(err)
			}
			creds.DestinationUser = ECRLogin
			creds.DestinationPassword = ECRPassword
		}
		if destinationRegistryType != "azure" && destinationRegistryType != "aws" && destinationRegistryType != "alicloud" && destinationRegistryType != "google" {
			if destinationRegistryType == "" {
				destinationRegistryType = "azure"
			} else {
				panic("unknown DESTINATION_REGISTRY_TYPE")
			}
		}
		docker.Replicate(creds, sourceRegistry, destinationRegistry, artifactFilter, destinationRegistryType)
		if len(docker.FailedPushRepos) != 0 || len(docker.FailedPullRepos) != 0 || len(docker.FailedCleanRepos) != 0 {
			log.Println("Failed docker operations:")
			if len(docker.FailedPushRepos) != 0 {
				log.Println("Docker push failed:")
				log.Println(docker.FailedPushRepos)
			}
			if len(docker.FailedPullRepos) != 0 {
				log.Println("Docker pull failed:")
				log.Println(docker.FailedPullRepos)
			}
			if len(docker.FailedCleanRepos) != 0 {
				log.Println("Docker clean failed:")
				log.Println(docker.FailedCleanRepos)
			}
			os.Exit(1)
		}
	} else if artifactType == "binary" {
		if destinationRegistryType != "s3" && destinationRegistryType != "artifactory" && destinationRegistryType != "oss" {
			panic("unknown or empty DESTINATION_REGISTRY_TYPE")
		}
		if artifactFilterProd == "" {
			binary.AlwaysSyncList = append(binary.AlwaysSyncList, "index.yaml")
		}
		syncPattern := os.Getenv("SYNC_PATTERN")
		if syncPattern != "" {
			log.Println("Sync Pattern:", syncPattern)
		}
		helmCdnDomain := os.Getenv("HELM_CDN_DOMAIN")
		if helmCdnDomain != "" {
			log.Println("Helm CDN domain: " + helmCdnDomain)
		}
		log.Println("Replicating dev repo")
		replicatedRealArtifacts, replicatedForcedArtifacts := binary.Replicate(creds, sourceRegistry, destinationRegistry, destinationRegistryType, artifactFilter, force, helmCdnDomain, syncPattern)
		log.Printf("%d real artifacts copied to %s\n", len(replicatedRealArtifacts), artifactFilter)
		log.Printf("%d forced artifacts copied to %s\n", len(replicatedForcedArtifacts), artifactFilter)
		var replicatedRealArtifactsProd []string
		var repoNameProd string
		repoName := strings.Split(artifactFilter, "/")[0]
		if len(artifactFilterProd) != 0 {
			repoNameProd = strings.Split(artifactFilterProd, "/")[0]
		}
		if artifactFilterProd != "" {
			log.Println("Replicating prod repo")
			replicatedRealArtifactsProd, replicatedForcedArtifacts = binary.Replicate(creds, sourceRegistry, destinationRegistry, destinationRegistryType, artifactFilterProd, force, helmCdnDomain, syncPattern)
		}
		if (len(replicatedRealArtifacts) != 0 || len(replicatedRealArtifactsProd) != 0) && artifactFilterProd != "" {
			err := helm.RegenerateIndexYaml(replicatedRealArtifacts, replicatedRealArtifactsProd, sourceRegistry, destinationRegistry, repoName, repoNameProd, helmCdnDomain)
			if err != nil {
				panic(err)
			}
		}
		if len(binary.FailedArtifactoryDownload) != 0 || len(binary.FailedS3Upload) != 0 {
			if len(binary.FailedS3Upload) != 0 {
				log.Println("S3 upload failed:")
				log.Println(binary.FailedS3Upload)
				err2 := slack.SendMessage("S3 upload failed")
				if err2 != nil {
					log.Println("slack.SendMessage failed")
					log.Println(err2)
				}
			}
			if len(binary.FailedArtifactoryDownload) != 0 {
				log.Println("Failed artifactory download:")
				log.Println(binary.FailedArtifactoryDownload)
				err2 := slack.SendMessage("Artifactory download failed")
				if err2 != nil {
					log.Println("slack.SendMessage failed")
					log.Println(err2)
				}
			}
			os.Exit(1)
		}
	} else {
		panic("unknown or empty ARTIFACT_TYPE")
	}
}
