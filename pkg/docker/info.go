package docker

import (
	"io/ioutil"
	"net/http"
	"regexp"
	"strings"
)

func getDockerCreateTime(dockerRegistry string, image string, tag string, user string, pass string) (string, error) {
	httpClient := &http.Client{}
	req, err := http.NewRequest("GET", "https://"+dockerRegistry+"/v2/"+image+"/manifests/"+tag, nil)
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(user, pass)
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	r, err := regexp.Compile("(created.+Z)")
	if err != nil {
		return "", err
	}
	created := r.FindAll(body, 999)
	var tsmax string
	for _, c := range created {
		cs := strings.Split(string(c), "\"")
		ts := cs[2]
		if ts > tsmax {
			tsmax = ts
		}
	}
	return tsmax, nil
}
