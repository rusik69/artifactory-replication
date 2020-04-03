package docker

import (
	"log"
	"os"
	"strings"
	"time"

	"github.com/loqutus/artifactory-replication/pkg/credentials"
)

func Clean(reposLimit string, sourceFilteredRepos []string, destinationFilteredRepos []string, artifactFilter string, destinationRegistry string, creds credentials.Creds, destinationRegistryType string) {
	log.Println("Cleaning repo:", destinationRegistry)
	sourceProdRegistry := os.Getenv("SOURCE_PROD_REGISTRY")
	if sourceProdRegistry == "" {
		panic("empty SOURCE_PROD_REGISTRY")
	}
	log.Println("Getting repos from prod source registry: " + sourceProdRegistry)
	prodSourceRegistryUser := os.Getenv("SOURCE_PROD_REGISTRY_USER")
	prodSourceRegistryPassword := os.Getenv("SOURCE_PROD_REGISTRY_PASSWORD")
	log.Println("I'm going to remove yesterday and older tags")
	sourceProdRepos, err := GetRepos(sourceProdRegistry, prodSourceRegistryUser, prodSourceRegistryPassword, reposLimit)
	if err != nil {
		panic(err)
	}
	var prodSourceFilteredRepos []string
	if artifactFilter != "" {
		for _, sourceRepo := range sourceProdRepos {
			if strings.HasPrefix(sourceRepo, artifactFilter) {
				prodSourceFilteredRepos = append(prodSourceFilteredRepos, sourceRepo)
			}
		}
	} else {
		sourceFilteredRepos = sourceProdRepos
	}
	log.Println("Found prod source repos: ", len(sourceProdRepos))
	dt := time.Now()
	dateNow := dt.Format("2006-01-02")
	log.Println("Date Now:", dateNow)
	for _, destinationRepo := range destinationFilteredRepos {
		log.Println("Processing destination repo:", destinationRepo)
		var filteredDestinationTags []string
		var repoProdFound bool
		for _, prodRepo := range sourceProdRepos {
			if prodRepo == destinationRepo {
				repoProdFound = true
				break
			}
		}
		destinationRepoTags, err := listTags(destinationRegistry, destinationRepo, creds.DestinationUser, creds.DestinationPassword)
		if err != nil {
			panic(err)
		}
		if repoProdFound {
			sourceProdRepoTags, err := listTags(sourceProdRegistry, destinationRepo, prodSourceRegistryUser, prodSourceRegistryPassword)
			if err != nil {
				panic(err)
			}
			for _, destinationTag := range destinationRepoTags {
				var tagFound bool
				for _, sourceProdTag := range sourceProdRepoTags {
					if destinationTag == sourceProdTag {
						log.Println("Found tag on prod source and destination:", destinationRepo+":"+destinationTag)
						tagFound = true
						break
					}
				}
				if !tagFound {
					filteredDestinationTags = append(filteredDestinationTags, destinationTag)
				}
			}
		} else {
			filteredDestinationTags = destinationRepoTags
		}
		timeTags := make(map[string]string)
		for _, destinationTag := range filteredDestinationTags {
			tagUploadDateTime, err := GetCreateTime(destinationRegistry, destinationRepo, destinationTag, creds.DestinationUser, creds.DestinationPassword)
			if err != nil {
				panic(err)
			}
			//log.Println("Getting tag creation time:", destinationRegistry+"/"+destinationRepo+":"+destinationTag, tagUploadDate)
			s := strings.Split(tagUploadDateTime, "T")
			tagUploadDate := s[0]
			timeTags[destinationTag] = tagUploadDate
		}

		for k, v := range timeTags {
			if v != dateNow {
				log.Println("Removing tag:", k, v)
				err := dockerRemoveTag(destinationRegistry, destinationRepo, k, destinationRegistryType, creds.DestinationUser, creds.DestinationPassword)
				if err != nil {
					panic(err)
				}
				RemovedTags++
			} else {
				log.Println("Keeping tag:", k, v)
				SkippedTags++
			}

		}
	}
	log.Println("Removed", RemovedTags, "tags")
	log.Println("Skipped", SkippedTags, "tags")
}
