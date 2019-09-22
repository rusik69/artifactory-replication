package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"io"
)

type Creds struct {
	SourceUser          string
	SourcePassword      string
	DestinationUser     string
	DestinationPassword string
}

type ImageToReplicate struct {
	SourceRegistry      string
	SourceImage         string
	DestinationRegistry string
	DestinationImage    string
	SourceTag           string
	DestinationTag      string
}

func GetToken(docker_registry string) (string, error) {
	resp, err := http.Get("https://" + docker_registry + "/artifactory/api/docker/docker-prod-local/v2/token")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	type res struct {
		Token string
		TTL   uint64
	}
	var b res
	err = json.Unmarshal(body, &b)
	if err != nil {
		return "", err
	}
	return string(b.Token), nil
}

func GetRepos(docker_registry string, token string) []string {
	client := &http.Client{}
	req, err := http.NewRequest("GET", "https://"+docker_registry+"/v2/_catalog", nil)
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

func listTags(docker_registry string, image string, token string) []string {
	httpClient := &http.Client{}
	req, err := http.NewRequest("GET", "https://"+docker_registry+"/v2/"+image+"/tags/list", nil)
	if err != nil {
		panic(err)
	}
	req.Header.Add("Authorization", "Bearer "+token)
	resp, err := httpClient.Do(req)
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}
	type res struct {
		Name string
		Tags []string
	}
	var b res
	err = json.Unmarshal(body, &b)
	if err != nil {
		panic(err)
	}
	return b.Tags
}

func pullImage(image ImageToReplicate, creds Creds) error {
	sourceImage := image.SourceRegistry + "/" + image.SourceImage + ":" + image.SourceTag
	ctx := context.Background()
	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		return err
	}
	cli.NegotiateAPIVersion(ctx)
	if creds.SourceUser != "" || creds.SourcePassword != "" {
		authConfig := types.AuthConfig{
			Username: creds.SourceUser,
			Password: creds.SourcePassword,
		}
		encodedJSON, err := json.Marshal(authConfig)
		if err != nil {
			return err
		}
		authStr := base64.URLEncoding.EncodeToString(encodedJSON)
		out, err := cli.ImagePull(ctx, sourceImage, types.ImagePullOptions{RegistryAuth: authStr})
		if err != nil {
			return err
		}
		io.Copy(ioutil.Discard, out)
		defer out.Close()
	} else {
		out, err := cli.ImagePull(ctx, sourceImage, types.ImagePullOptions{})
		if err != nil {
			return err
		}
		io.Copy(ioutil.Discard, out)
		defer out.Close()
	}
	return nil
}

func pushImage(image ImageToReplicate, creds Creds) error {
	destinationImage := image.DestinationRegistry + "/" + image.DestinationImage + ":" + image.DestinationTag
	sourceImage := image.SourceRegistry + "/" + image.SourceImage + ":" + image.SourceTag
	ctx := context.Background()
	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		return err
	}
	cli.NegotiateAPIVersion(ctx)
	err = cli.ImageTag(ctx, sourceImage, destinationImage)
	if err != nil {
		return err
	}
	if creds.DestinationUser != "" || creds.DestinationPassword != "" {
		authConfig := types.AuthConfig{
			Username: creds.DestinationUser,
			Password: creds.DestinationPassword,
		}
		encodedJSON, err := json.Marshal(authConfig)
		if err != nil {
			return err
		}
		authStr := base64.URLEncoding.EncodeToString(encodedJSON)
		out, err := cli.ImagePush(ctx, destinationImage, types.ImagePushOptions{RegistryAuth: authStr})
		if err != nil {
			return err
		}
		io.Copy(ioutil.Discard, out)
		defer out.Close()
	} else {
		out, err := cli.ImagePush(ctx, destinationImage, types.ImagePushOptions{})
		if err != nil {
			return err
		}
		io.Copy(ioutil.Discard, out)
		defer out.Close()
	}
	return nil
}

func replicate(image ImageToReplicate, creds Creds, token string) error {
	destinationImage := image.DestinationRegistry + "/" + image.DestinationImage + ":" + image.DestinationTag
	sourceImage := image.SourceRegistry + "/" + image.SourceImage + ":" + image.SourceTag
	fmt.Printf("%s -> %s\n", sourceImage, destinationImage)
	err := pullImage(image, creds)
	if err != nil {
		return err
	}
	err = pushImage(image, creds)
	if err != nil {
		return err
	}
	return nil
}

func main() {
	sourceRegistry := os.Getenv("SOURCE_REGISTRY")
	if sourceRegistry == "" {
		panic("empty SOURCE_REGISTRY env variable")
	}
	destinationRegistry := os.Getenv("DESTINATION_REGISTRY")
	if destinationRegistry == "" {
		panic("empty DESTINATION_REGISTRY env variable")
	}
	creds := Creds{
		SourceUser:          os.Getenv("SOURCE_USER"),
		SourcePassword:      os.Getenv("SOURCE_PASSWORD"),
		DestinationUser:     os.Getenv("DESTINATION_USER"),
		DestinationPassword: os.Getenv("DESTINATION_PASSWORD"),
	}
	imageFilter := os.Getenv("IMAGE_FILTER")
	token, err := GetToken(sourceRegistry)
	if err != nil{
		panic(err)
	}
	repos := GetRepos(sourceRegistry, token)
	if err != nil{
		panic(err)
	}
	resultRepos := repos[:0]
	if imageFilter != "" {
		for _, repo := range repos {
			if strings.HasPrefix(repo, imageFilter) {
				resultRepos = append(resultRepos, repo)
			}
		}
	} else {
		resultRepos = repos
	}
	for _, repo := range resultRepos {
		for _, tag := range listTags(sourceRegistry, repo, token) {
			image := ImageToReplicate{
				SourceRegistry:      sourceRegistry,
				SourceImage:         repo,
				DestinationRegistry: destinationRegistry,
				DestinationImage:    repo,
				SourceTag:           tag,
				DestinationTag:      tag,
			}
			err := replicate(image, creds, token)
			if err != nil {
				panic(err)
			}
		}
	}
}
