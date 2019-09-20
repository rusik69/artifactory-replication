package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
)

func get_token(docker_registry string) string {
	resp, err := http.Get(docker_registry + "/artifactory/api/docker/docker-prod-local/v2/token")
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}
	type res struct {
		Token string
		TTL   uint64
	}
	var b res
	err = json.Unmarshal(body, &b)
	if err != nil {
		panic(err)
	}
	return (b.Token)
}

func get_repos(docker_registry string, token string) []string {
	client := &http.Client{}
	req, err := http.NewRequest("GET", docker_registry+"/v2/_catalog", nil)
	if err != nil {
		panic(err)
	}
	req.Header.Add("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}
	type res struct {
		Repositories []string
	}
	var b res
	err = json.Unmarshal(body, &b)
	if err != nil {
		panic(err)
	}
	return b.Repositories
}

func main() {
	dockerRegistry := os.Getenv("DOCKER_REGISTRY")
	if dockerRegistry == "" {
		panic("empty DOCKER_REGISTRY env variable")
	}
	imageFilter := os.Getenv("IMAGE_FILTER")
	token := get_token(dockerRegistry)
	repos := get_repos(dockerRegistry, token)
	resultRepos := repos[:0]
	if imageFilter != "" {
		for _, repo := range repos {
			if strings.HasPrefix(repo, imageFilter){
				resultRepos = append(resultRepos, repo)
			}
		}
	} else {
		resultRepos = repos
	}
	for _, repo :=  range resultRepos{
		fmt.Println(repo)
	}
}
