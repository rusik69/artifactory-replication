package docker

import (
	"errors"
	"io/ioutil"
	"log"
	"net/http"
	"regexp"
	"strings"
)

func getAzureDockerTagManifestDigest(registry string, image string, tag string, user string, pass string) (string, error) {
	log.Println("Getting tag manifest digest:", image+":"+tag)
	url := "https://" + registry + "/acr/v1/" + image + "/_manifests"
	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(user, pass)
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if strings.Contains(string([]byte(body)), "error") || strings.Contains(string([]byte(body)), "Error") {
		return "", errors.New(string([]byte(body)))
	}
	r, err := regexp.Compile("sha256:(.+?),")
	if err != nil {
		return "", err
	}
	digestRaw := r.Find(body)
	if string(digestRaw) == "" {
		return "", nil
	}
	digestSha := digestRaw[0 : len(digestRaw)-2]
	//digest := digestSha[7:]
	digest := digestSha
	return string(digest), nil
}
