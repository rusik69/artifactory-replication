package docker

import (
	"encoding/json"
	"errors"
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
	for i := 1; i <= backOffSteps; i++ {
		resp, err = client.Do(req)
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
		return nil, err
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if strings.Contains(string([]byte(body)), "errors") {
		return nil, errors.New(string([]byte(body)))
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
