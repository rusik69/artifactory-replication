package repos

import (
	"log"
	"os"

	"github.com/loqutus/artifactory-replication/pkg/binary"
	"github.com/loqutus/artifactory-replication/pkg/credentials"
	"github.com/loqutus/artifactory-replication/pkg/docker"
	"github.com/loqutus/artifactory-replication/pkg/slack"
)

func Check(sourceRegistry string, destinationRegistry string, creds credentials.Creds, artifactType string, destinationRegistryType string, dir string) {
	log.Println("Checking " + destinationRegistryType + " repo consistency between " + sourceRegistry + " and " + destinationRegistry)
	var slackMessage string
	if artifactType == "docker" {
		err := docker.CheckRepos(sourceRegistry, destinationRegistry, destinationRegistryType, creds)
		if err != nil {
			err := slack.SendMessage(err.Error())
			if err != nil {
				panic(err)
			}
		}
		if docker.CheckFailed {
			if len(docker.MissingRepos) > 0 {
				log.Println("Consistency check failed, missing docker repos:")
				slackMessage += "Consistency check failed, missing docker repos:\n"
				for _, missingRepo := range docker.MissingRepos {
					log.Println(missingRepo)
					slackMessage += missingRepo + "\n"
				}
			}
			if len(docker.MissingRepoTags) > 0 {
				log.Println("Consistency check failed, missing docker tags:")
				slackMessage += "Consistency check failed, missing docker tags:\n"
				for _, missingRepoTag := range docker.MissingRepoTags {
					log.Println(missingRepoTag)
					slackMessage += missingRepoTag + "\n"
				}
			}
			err := slack.SendMessage(slackMessage)
			if err != nil {
				panic(err)
			}
			os.Exit(1)
		} else {
			log.Println("No missing repos found")
			return
		}
	} else if artifactType == "binary" {
		err := binary.CheckRepos(sourceRegistry, destinationRegistry, destinationRegistryType, creds, dir)
		if err != nil {
			err := slack.SendMessage(err.Error())
			if err != nil {
				panic(err)
			}
		}
		if binary.CheckFailed {
			log.Println("Repo check failed, files not found in destination:")
			log.Println(binary.CheckFailedList)
			slackMessage += "Repo check failed, files not found in destination:\n"
			for _, file := range binary.CheckFailedList {
				slackMessage += file + "\n"
			}
			err := slack.SendMessage(slackMessage)
			if err != nil {
				panic(err)
			}
			os.Exit(1)
		}
	}
}
