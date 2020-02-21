package docker

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
)

func listTags(dockerRegistry string, image string, user string, pass string) ([]string, error) {
	httpClient := &http.Client{}
	req, err := http.NewRequest("GET", "https://"+dockerRegistry+"/v2/"+image+"/tags/list?n=10000000", nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(user, pass)
	resp, err := httpClient.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	type res struct {
		Name string
		Tags []string
	}
	var b res
	err = json.Unmarshal(body, &b)
	if err != nil {
		log.Println("docker registry response json unmarshall error")
		return nil, err
	}
	return b.Tags, nil
}

func dockerRemoveTag(registry string, image string, tag string, destinationRegistryType string, user string, pass string) error {
	if destinationRegistryType == "azure" {
		digest, err := getAzureDockerTagManifestDigest(registry, image, tag, user, pass)
		if err != nil {
			return err
		}
		log.Println("Removing tag:", registry+"/"+image+":"+tag)
		url := "https://" + registry + "/acr/v1/" + image + "/_tags/" + tag
		client := &http.Client{}
		req, err := http.NewRequest("DELETE", url, nil)
		if err != nil {
			return err
		}
		req.SetBasicAuth(user, pass)
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		if strings.Contains(string([]byte(body)), "error") || strings.Contains(string([]byte(body)), "Error") {
			log.Println("Error removing tag", image+":"+tag)
			log.Println(string([]byte(body)))
			log.Println("Ignoring...")
			skippedTags++
			return nil
		}
		if digest != "" {
			log.Println("Removing", image+":"+tag, "digest:", digest)
			urlTag := "https://" + registry + "/v2/" + image + "/manifests/" + digest
			clientTag := &http.Client{}
			reqTag, err := http.NewRequest("DELETE", urlTag, nil)
			if err != nil {
				return err
			}
			reqTag.SetBasicAuth(user, pass)
			respTag, err := clientTag.Do(reqTag)
			if err != nil {
				return err
			}
			defer respTag.Body.Close()
			bodyTag, err := ioutil.ReadAll(respTag.Body)
			if err != nil {
				return err
			}
			if strings.Contains(string([]byte(bodyTag)), "error") || strings.Contains(string([]byte(bodyTag)), "Error") {
				log.Println("Error removing tag", image+":"+tag)
				log.Println(string([]byte(bodyTag)))
				log.Println("Ignoring...")
				skippedTags++
				return nil
			}
		} else {
			log.Println("Tag", image+":"+tag, "have empty digest, skipping...")
			skippedTags++
			return nil
		}
	} else {
		log.Println("Unknown destination registry type:", destinationRegistryType)
		return errors.New("unknown destination registry type")
	}
	log.Println("Removed tag:", registry+"/"+image+":"+tag)
	removedTags++
	return nil
}
