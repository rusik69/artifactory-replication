package docker

import (
	"log"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ecr"
	"github.com/loqutus/artifactory-replication/pkg/credentials"
	"github.com/loqutus/artifactory-replication/pkg/slack"
)

var FailedPullRepos []string
var FailedPushRepos []string
var FailedCleanRepos []string

// ImageToReplicate source/desination image parameters
type ImageToReplicate struct {
	SourceRegistry      string
	SourceImage         string
	DestinationRegistry string
	DestinationImage    string
	SourceTag           string
	DestinationTag      string
}

func doReplicateDocker(image ImageToReplicate, creds credentials.Creds, destinationRegistryType string, repoFound *bool, dockerRepoPrefix string) error {
	err := pullImage(image, creds)
	if err != nil {
		log.Println(err)
		log.Println("Error pulling image, ignoring...")
		FailedPullRepos = append(FailedPullRepos, image.SourceImage+":"+image.SourceTag)
		return nil
	}
	if destinationRegistryType == "aws" && *repoFound == false {
		log.Println("Creating destination repo: " + image.DestinationImage)
		input := ecr.CreateRepositoryInput{
			RepositoryName: &image.DestinationImage,
		}
		sess, _ := session.NewSession(&aws.Config{})
		svc := ecr.New(sess)
		output, err := svc.CreateRepository(&input)
		if err != nil {
			if !strings.Contains(err.Error(), "already exists") {
				log.Println(output.String())
				return err
			}
		}
		*repoFound = true
	} else if destinationRegistryType == "alicloud" || destinationRegistryType == "google" {
		if dockerRepoPrefix != "" {
			image.DestinationImage = dockerRepoPrefix + "/" + image.DestinationImage
		}
	}
	destinationImage := image.DestinationRegistry + "/" + image.DestinationImage + ":" + image.DestinationTag
	sourceImage := image.SourceRegistry + "/" + image.SourceImage + ":" + image.SourceTag
	err = pushImage(image, creds)
	if err != nil {
		log.Println(err)
		log.Println("Error pushing image, ignoring...")
		FailedPushRepos = append(FailedPushRepos, image.DestinationImage+":"+image.DestinationTag)
		return nil
	}
	err = DeleteImage(sourceImage)
	if err != nil {
		log.Println(err)
		log.Println("Error deleting local image", sourceImage, ", ignoring...")
		FailedCleanRepos = append(FailedCleanRepos, image.SourceImage+":"+image.SourceTag)
		return nil
	}
	err = DeleteImage(destinationImage)
	if err != nil {
		log.Println(err)
		log.Println("error deleting local image ", destinationImage, ", ignoring...")
		FailedCleanRepos = append(FailedCleanRepos, image.DestinationImage+":"+image.DestinationTag)
		return nil
	}
	return nil
}

func Replicate(creds credentials.Creds, sourceRegistry string, destinationRegistry string, artifactFilter string, destinationRegistryType string) {
	var copiedArtifacts uint = 0
	var reposLimit string
	if destinationRegistryType == "aws" {
		reposLimit = "1000"
	} else {
		reposLimit = "1000000"
	}
	log.Println("Getting repos from source registry: " + sourceRegistry)
	sourceRepos, err := GetRepos(sourceRegistry, creds.SourceUser, creds.SourcePassword, reposLimit)
	if err != nil {
		err2 := slack.SendMessage(err.Error())
		if err2 != nil {
			log.Println(err)
			panic(err2)
		}
		panic(err)
	}
	log.Println("Found source repos: ", len(sourceRepos))
	log.Println("Getting repos from destination from destination registry: " + destinationRegistry)
	destinationRepos, err := GetRepos(destinationRegistry, creds.DestinationUser, creds.DestinationPassword, reposLimit)
	if err != nil {
		err2 := slack.SendMessage(err.Error())
		if err2 != nil {
			log.Println(err)
			panic(err2)
		}
		panic(err)
	}
	log.Println("Found destination repos: ", len(destinationRepos))
	dockerRepoPrefix := os.Getenv("DOCKER_REPO_PREFIX")
	dockerTag := os.Getenv("DOCKER_TAG")
	sourceFilteredRepos := sourceRepos[:0]
	if artifactFilter != "" {
		for _, sourceRepo := range sourceRepos {
			if strings.HasPrefix(sourceRepo, artifactFilter) {
				sourceFilteredRepos = append(sourceFilteredRepos, sourceRepo)
			}
		}
	} else {
		sourceFilteredRepos = sourceRepos
	}
	log.Println("Found filtered source repos: ", len(sourceFilteredRepos))
	destinationFilteredRepos := destinationRepos[:0]
	if artifactFilter != "" {
		for _, sourceRepo := range destinationRepos {
			if strings.HasPrefix(sourceRepo, artifactFilter) {
				destinationFilteredRepos = append(destinationFilteredRepos, sourceRepo)
			}
		}
	} else {
		destinationFilteredRepos = destinationRepos
	}
	log.Println("Found filtered destination repos: ", len(destinationFilteredRepos))
	dockerCleanup := os.Getenv("DOCKER_CLEAN")
	if dockerCleanup == "true" {
		Clean(reposLimit, sourceFilteredRepos, destinationFilteredRepos, artifactFilter, destinationRegistry, creds, destinationRegistryType)
		return
	}
	for _, sourceRepo := range sourceFilteredRepos {
		sourceTags, err := listTags(sourceRegistry, sourceRepo, creds.SourceUser, creds.SourcePassword)
		if err != nil {
			err2 := slack.SendMessage(err.Error())
			if err2 != nil {
				log.Println(err)
				panic(err2)
			}
			panic(err)
		}
		var sourceTagsFiltered []string
		if dockerTag != "" {
			for _, sourceTag := range sourceTags {
				if sourceTag == dockerTag {
					sourceTagsFiltered = append(sourceTagsFiltered, sourceTag)
				}
			}
		} else {
			sourceTagsFiltered = sourceTags
		}
		repoFound := false
		for _, destinationRepo := range destinationFilteredRepos {
			if destinationRegistryType == "google" {
				destinationRepo = strings.TrimPrefix(destinationRepo, dockerRepoPrefix+"/")
			}
			if sourceRepo == destinationRepo {
				repoFound = true
				//log.Println("Found repo: " + sourceRepo)
				break
			}
		}
		for _, sourceTag := range sourceTagsFiltered {
			image := ImageToReplicate{
				SourceRegistry:      sourceRegistry,
				SourceImage:         sourceRepo,
				DestinationRegistry: destinationRegistry,
				DestinationImage:    sourceRepo,
				SourceTag:           sourceTag,
				DestinationTag:      sourceTag,
			}
			if !repoFound {
				log.Println("Destination repo not found: " + sourceRepo)
				err := doReplicateDocker(image, creds, destinationRegistryType, &repoFound, dockerRepoPrefix)
				if err != nil {
					err2 := slack.SendMessage(err.Error())
					if err2 != nil {
						log.Println(err)
						panic(err2)
					}
					panic(err)
				}
				copiedArtifacts++
			} else {
				destinationTagFound := false
				destinationRepo := sourceRepo
				if dockerRepoPrefix != "" {
					destinationRepo = dockerRepoPrefix + "/" + sourceRepo
				}
				destinationTags, err := listTags(destinationRegistry, destinationRepo, creds.DestinationUser, creds.DestinationPassword)
				if err != nil {
					err2 := slack.SendMessage(err.Error())
					if err2 != nil {
						log.Println(err)
						panic(err2)
					}
					panic(err)
				}
				for _, destinationTag := range destinationTags {
					if sourceTag == destinationTag {
						destinationTagFound = true
						//log.Println("Found repo tag: " + sourceRepo + ":" + sourceTag)
						break
					}
				}
				if destinationTagFound {
					continue
				} else {
					log.Println("Repo tag: " + sourceRepo + ":" + sourceTag + " not found at destination, replicating...")
					err := doReplicateDocker(image, creds, destinationRegistryType, &repoFound, dockerRepoPrefix)
					if err != nil {
						err2 := slack.SendMessage(err.Error())
						if err2 != nil {
							log.Println(err)
							panic(err2)
						}
						panic(err)
					}
					copiedArtifacts++
				}
			}
		}
	}
	log.Printf("%d artifacts copied\n", copiedArtifacts)
}
