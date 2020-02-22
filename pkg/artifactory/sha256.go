package artifactory

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
)

func GetArtifactoryFileSHA256(host string, fileName string, user string, pass string) (string, error) {
	url := "https://" + host + "/artifactory/api/storage/" + fileName
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
	type storageInfo struct {
		Checksums map[string]string `json:"checksums"`
	}
	var result storageInfo
	err = json.Unmarshal(body, &result)
	if err != nil {
		return "", err
	}
	return result.Checksums["sha256"], nil
}
