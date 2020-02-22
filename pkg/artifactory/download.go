package artifactory

import (
	"errors"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"regexp"
)

func Download(fileURL string, helmCdnDomain string) (string, error) {
	log.Println("Downloading " + fileURL)
	resp, err := http.Get(fileURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	tempFile, err := ioutil.TempFile("", "artifactory-download")
	if err != nil {
		return "", err
	}
	if resp.StatusCode != 200 {
		return "", errors.New("Response code error: " + string(resp.StatusCode))
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
