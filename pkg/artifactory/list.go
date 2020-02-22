package artifactory

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"strings"
)

func ListFiles(host string, dir string, user string, pass string) (map[string]bool, error) {
	url := "https://" + host + "/artifactory/api/storage/" + dir
	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(user, pass)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
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
