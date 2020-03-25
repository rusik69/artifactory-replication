package docker

import (
	"io/ioutil"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"
)

func GetCreateTime(dockerRegistry string, image string, tag string, user string, pass string) (string, error) {
	httpClient := &http.Client{}
	url := "https://" + dockerRegistry + "/v2/" + image + "/manifests/" + tag
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(user, pass)
	var resp *http.Response
	var failed bool
	backOffTime := backOffStart
	for i := 1; i <= backOffSteps; i++ {
		resp, err = httpClient.Do(req)
		defer resp.Body.Close()
		if err != nil {
			failed = true
			log.Print("error HTTP GET", url, "retry", string(i))
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
		return "", err
	}
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
