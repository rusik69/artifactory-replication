package artifactory

import (
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"regexp"
	"time"
)

func Download(fileURL string, helmCdnDomain string) (string, error) {
	log.Println("Downloading " + fileURL)
	var resp *http.Response
	var failed bool
	var err error
	backOffTime := backOffStart
	for i := 1; i <= backOffSteps; i++ {
		resp, err = http.Get(fileURL)
		if err != nil || resp.StatusCode != 200 {
			failed = true
			log.Println("error HTTP GET", resp.Status, fileURL, "retry", string(i))
			if i != backOffSteps {
				time.Sleep(time.Duration(backOffTime) * time.Millisecond)
			}
			backOffTime *= i
		} else {
			defer resp.Body.Close()
			failed = false
			break
		}
	}
	if failed == true {
		return "", err
	}
	tempFile, err := ioutil.TempFile("", "artifactory-download")
	if err != nil {
		return "", err
	}
	_, err = io.Copy(tempFile, resp.Body)
	if err != nil {
		return "", err
	}
	fileName := tempFile.Name()
	tempFile.Close()
	matched, err := regexp.MatchString("/index.yaml$", fileURL)
	if err != nil {
		return "", err
	}
	if matched && helmCdnDomain != "" {
		body, err := ioutil.ReadFile(fileName)
		if err != nil {
			return "", err
		}
		linkToReplace, err := regexp.Compile("(https?://.*?artifactory.*?/(artifactory/)?[^/]*?/)")
		if err != nil {
			return "", err
		}
		log.Println("Rewriting index.yaml urls...")
		body = linkToReplace.ReplaceAll(body, []byte("https://"+helmCdnDomain+"/"))
		err = ioutil.WriteFile(fileName, body, os.FileMode(0644))
		if err != nil {
			return "", err
		}
	}
	return fileName, nil
}
