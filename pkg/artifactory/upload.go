package artifactory

import (
	"log"
	"net/http"
	"os"
	"time"
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
	var failed bool
	backOffTime := backOffStart
	for i := 1; i <= backOffSteps; i++ {
		_, err = client.Do(req)
		if err != nil {
			failed = true
			log.Print("error HTTP POST", url, "retry", string(i))
			if i != backOffSteps {
				time.Sleep(time.Duration(backOffTime) * time.Millisecond)
			}
			backOffTime *= i
		} else {
			failed = false
			break
		}
	}
	if failed == true {
		return err
	}
	return err
}
