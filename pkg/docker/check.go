package docker

import (
	"log"

	"github.com/loqutus/artifactory-replication/pkg/credentials"
)

var checkFailed bool
var checkFailedList []string
var missingRepos, missingRepoTags []string
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
			checkFailed = true
			missingRepos = append(missingRepos, sourceRepo)
		}
		if !checkFailed {
			sourceRepoTags, err := listTags(sourceRegistry, sourceRepo, creds.SourceUser, creds.SourcePassword)
			if err != nil {
				log.Println("Failed to get tags for repo: " + sourceRepo)
				missingRepos = append(missingRepos, sourceRepo)
				checkFailed = true
			}
			destinationRepoTags, err := listTags(destinationRegistry, sourceRepo, creds.DestinationUser, creds.DestinationPassword)
			if err != nil {
				log.Println("Failed to get tags for repo: " + sourceRepo)
				missingRepos = append(missingRepos, sourceRepo)
				checkFailed = true
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
					missingRepoTags = append(missingRepoTags, sourceRepo+":"+sourceRepoTag)
					checkFailed = true
				}
			}
		}
	}
	return nil
}
