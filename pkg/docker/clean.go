package docker

import (
	"log"
	"os"
	"sort"
	"strconv"
	"strings"

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
	dockerCleanKeepTagsString := os.Getenv("DOCKER_CLEAN_KEEP_TAGS")
	if dockerCleanKeepTagsString == "" {
		dockerCleanKeepTagsString = "10"
	}
	log.Println("I'm going to remove last", dockerCleanKeepTagsString, "tags")
	dockerCleanKeepTags, err := strconv.Atoi(dockerCleanKeepTagsString)
	if err != nil {
		panic(err)
	}
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
		var values []string
		for _, destinationTag := range filteredDestinationTags {
			tagUploadDate, err := GetCreateTime(destinationRegistry, destinationRepo, destinationTag, creds.DestinationUser, creds.DestinationPassword)
			if err != nil {
				panic(err)
			}
			log.Println("Getting tag creation time:", destinationRegistry+"/"+destinationRepo+":"+destinationTag, tagUploadDate)
			timeTags[destinationTag] = tagUploadDate
			values = append(values, tagUploadDate)
		}
		sort.Strings(values)
		if len(values) > dockerCleanKeepTags {
			log.Println("Removing last", len(values)-dockerCleanKeepTags, "tags from:", destinationRegistry+"/"+destinationRepo)
			for i := len(values) - 1; i >= dockerCleanKeepTags; i-- {
				var tagToRemove string
				for k, v := range timeTags {
					if values[i] == v {
						tagToRemove = k
					}
				}
				err := dockerRemoveTag(destinationRegistry, destinationRepo, tagToRemove, destinationRegistryType, creds.DestinationUser, creds.DestinationPassword)
				if err != nil {
					panic(err)
				}
			}
		}
	}
	log.Println("Removed", RemovedTags, "tags")
	log.Println("Skipped", SkippedTags, "tags")
}
