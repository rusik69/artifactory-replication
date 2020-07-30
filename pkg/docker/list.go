package docker

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
	"time"
)

func GetRepos(dockerRegistry string, user string, pass string, reposLimit string) ([]string, error) {
	client := &http.Client{}
	url := "https://" + dockerRegistry + "/v2/_catalog?n=" + reposLimit
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(user, pass)
	var resp *http.Response
	var failed bool
	backOffTime := backOffStart
	var body []byte
	for i := 1; i <= backOffSteps; i++ {
		resp, err = client.Do(req)
		if err != nil {
			failed = true
			log.Print("error HTTP GET", url, "retry", string(i))
			if i != backOffSteps {
				time.Sleep(time.Duration(backOffTime) * time.Millisecond)
			}
			backOffTime *= i
		} else {
			defer resp.Body.Close()
			failed = false
			body, err = ioutil.ReadAll(resp.Body)
			if err != nil || strings.Contains(string([]byte(body)), "errors") {
				failed = true
				log.Print("error HTTP GET", url, "retry", string(i))
				backOffTime *= i
			}
			break
		}
	}
	if failed == true {
		return nil, err
	}
	type res struct {
		Repositories []string
	}
	var b res
	err = json.Unmarshal(body, &b)
	if err != nil {
		return nil, err
	}
	return b.Repositories, nil
}
