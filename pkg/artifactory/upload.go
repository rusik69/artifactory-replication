package artifactory

import (
	"log"
	"net/http"
	"os"
)

func Upload(destinationRegistry string, sourceRepo string, destinationFileName string, destinationUser string, destinationPassword string, tempFileName string) error {
	url := "https://" + destinationRegistry + "/artifactory/" + sourceRepo + destinationFileName
	log.Println("Uploading: " + url)
	f, err := os.Open(tempFileName)
	if err != nil {
		return err
	}
	client := &http.Client{}
	req, err := http.NewRequest(http.MethodPut, url, f)
	if err != nil {
		return err
	}
	req.SetBasicAuth(destinationUser, destinationPassword)
	_, err = client.Do(req)
	return err
}
