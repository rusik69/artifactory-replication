package artifactory

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
	"time"
)

func ListFiles(host string, dir string, user string, pass string) (map[string]bool, error) {
	url := "https://" + host + "/artifactory/api/storage/" + dir
	client := &http.Client{}
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
			defer resp.Body.Close()
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
	type ch struct {
		URI    string
		Folder bool
	}
	type storageInfo struct {
		Repo         string
		Path         string
		Created      string
		CreatedBy    string
		LastModified string
		ModifiedBy   string
		LastUpdated  string
		Children     []ch
		URI          string
	}
	var result storageInfo
	err = json.Unmarshal(body, &result)
	if err != nil {
		return nil, err
	}
	var output = make(map[string]bool)
	for _, file := range result.Children {
		fileNameWithPath := strings.Trim(result.Path, "/") + file.URI
		output[fileNameWithPath] = file.Folder
	}
	return output, nil
}
