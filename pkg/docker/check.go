package docker

import (
	"log"

	"github.com/loqutus/artifactory-replication/pkg/credentials"
)

var CheckFailed bool
var CheckFailedList []string
var MissingRepos, missingRepoTags []string
var removedTags, skippedTags uint64

func CheckRepos(sourceRegistry string, destinationRegistry string, destinationRegistryType string, creds credentials.Creds) error {
	var reposLimit string
	if destinationRegistryType == "aws" {
		reposLimit = "1000"
	} else {
		reposLimit = "1000000"
	}
	log.Println("Getting source repos from: " + sourceRegistry)
	sourceRepos, err := getRepos(sourceRegistry, creds.SourceUser, creds.SourcePassword, "1000000")
	if err != nil {
		return err
	}
	log.Println("Getting destination repos from: " + destinationRegistry)
	destinationRepos, err := getRepos(destinationRegistry, creds.DestinationUser, creds.DestinationPassword, reposLimit)
	if err != nil {
		return err
	}
	for _, sourceRepo := range sourceRepos {
		var destinationRepoFound bool
		for _, destinationRepo := range destinationRepos {
			if sourceRepo == destinationRepo {
				log.Println("Repo " + sourceRepo + " found")
				destinationRepoFound = true
				break
			}
		}
		if !destinationRepoFound {
			log.Println("Repo " + sourceRepo + " NOT found")
			CheckFailed = true
			MissingRepos = append(missingRepos, sourceRepo)
		}
		if !CheckFailed {
			sourceRepoTags, err := listTags(sourceRegistry, sourceRepo, creds.SourceUser, creds.SourcePassword)
			if err != nil {
				log.Println("Failed to get tags for repo: " + sourceRepo)
				MissingRepos = append(missingRepos, sourceRepo)
				CheckFailed = true
			}
			destinationRepoTags, err := listTags(destinationRegistry, sourceRepo, creds.DestinationUser, creds.DestinationPassword)
			if err != nil {
				log.Println("Failed to get tags for repo: " + sourceRepo)
				MissingRepos = append(missingRepos, sourceRepo)
				CheckFailed = true
			}
			for _, sourceRepoTag := range sourceRepoTags {
				tagFound := false
				for _, destinationRepoTag := range destinationRepoTags {
					if sourceRepoTag == destinationRepoTag {
						log.Println("Repo tag: " + sourceRepo + ":" + sourceRepoTag + " found")
						tagFound = true
						break
					}
				}
				if !tagFound {
					log.Println("Tag not found: " + sourceRepoTag)
					MissingRepoTags = append(missingRepoTags, sourceRepo+":"+sourceRepoTag)
					CheckFailed = true
				}
			}
		}
	}
	return nil
}
