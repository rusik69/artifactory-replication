package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"bytes"
	"errors"
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

func GetToken(docker_registry string, user string, password string, registryType string) (string, error) {
	var url string
	if registryType == "artifactory"{
		url = "https://" + docker_registry + "/artifactory/api/docker/docker-prod-local/v2/token"
	} else if registryType=="docker" {
		url = "https://" + docker_registry + "/v2/token"
	} else {
		return "", errors.New("unknown registry type: " + registryType)
	}
	var body []byte
	if user == "" && password == "" {
		resp, err := http.Get(url)
		if err != nil {
			return "", err
		}
		body, err = ioutil.ReadAll(resp.Body)
		if err != nil {
			return "", err
		}
	} else {
		type creds struct {
			username string
			password  string
		}
		credsJson := creds{user, password}
		credsString, err := json.Marshal(credsJson)
		if err != nil{
			return "", err
		}
		resp, err := http.Post(url,"application/json", bytes.NewBuffer(credsString))
		if err != nil{
			return "", err
		}
		body, err = ioutil.ReadAll(resp.Body)
		if err != nil {
			return "", err
		}
	}
	type res struct {
		Token string
		TTL   uint64
	}
	var b res
	err := json.Unmarshal(body, &b)
	if err != nil {
		return "", err
	}
	return string(b.Token), nil
}

func GetRepos(docker_registry string, token string) ([]string, error) {
	client := &http.Client{}
	req, err := http.NewRequest("GET", "https://"+docker_registry+"/v2/_catalog", nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Add("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
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

func listTags(docker_registry string, image string, token string) ([]string, error) {
	httpClient := &http.Client{}
	req, err := http.NewRequest("GET", "https://"+docker_registry+"/v2/"+image+"/tags/list", nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Add("Authorization", "Bearer "+token)
	}
	resp, err := httpClient.Do(req)
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
		return nil, err
	}
	return b.Tags, nil
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
		defer out.Close()
	}
	return nil
}

func deleteImage(imageName string) error {
	ctx := context.Background()
	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		return err
	}
	cli.NegotiateAPIVersion(ctx)
	il, err := cli.ImageList(ctx, types.ImageListOptions{})
	if err != nil {
		return err
	}
	for _, image := range il {
		for _, tag := range image.RepoTags {
			if tag == imageName {
				_, err := cli.ImageRemove(ctx, image.ID, types.ImageRemoveOptions{Force: true})
				if err != nil {
					return err
				}
			}
		}
	}
	return err
}

func replicate(image ImageToReplicate, creds Creds) error {
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
	err = deleteImage(sourceImage)
	if err != nil {
		return err
	}
	err = deleteImage(destinationImage)
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
	sourceRegistryType := os.Getenv("SOURCE_REGISTRY_TYPE")
	if sourceRegistryType == "" {
		panic("empty SOURCE_REGISTRY_TYPE env variable")
	}
	destinationRegistry := os.Getenv("DESTINATION_REGISTRY")
	if destinationRegistry == "" {
		panic("empty DESTINATION_REGISTRY env variable")
	}
	destinationRegistryType := os.Getenv("DESTINATION_REGISTRY_TYPE")
	if destinationRegistryType == "" {
		panic("empty DESTINATION_REGISTRY_TYPE env variable")
	}
	creds := Creds{
		SourceUser:          os.Getenv("SOURCE_USER"),
		SourcePassword:      os.Getenv("SOURCE_PASSWORD"),
		DestinationUser:     os.Getenv("DESTINATION_USER"),
		DestinationPassword: os.Getenv("DESTINATION_PASSWORD"),
	}
	imageFilter := os.Getenv("IMAGE_FILTER")
	sourceToken, err := GetToken(sourceRegistry, creds.SourceUser, creds.SourcePassword, sourceRegistryType)
	if err != nil {
		panic(err)
	}
	destinationToken, err := GetToken(sourceRegistry, creds.DestinationUser, creds.DestinationPassword, destinationRegistryType)
	if err != nil {
		panic(err)
	}
	fmt.Println(destinationToken)
	sourceRepos, err := GetRepos(sourceRegistry, sourceToken)
	if err != nil {
		panic(err)
	}
	/*destinationRepos, err := GetRepos(destinationRegistry, destinationToken)
	if err != nil {
		panic(err)
	}*/
	sourceFilteredRepos := sourceRepos[:0]
	if imageFilter != "" {
		for _, repo := range sourceRepos {
			if strings.HasPrefix(repo, imageFilter) {
				sourceFilteredRepos = append(sourceFilteredRepos, repo)
			}
		}
	} else {
		sourceFilteredRepos = sourceRepos
	}
	/*destinationFilteredRepos := destinationRepos[:0]
	if imageFilter != "" {
		for _, repo := range destinationRepos {
			if strings.HasPrefix(repo, imageFilter) {
				destinationFilteredRepos = append(destinationFilteredRepos, repo)
			}
		}
	} else {
		destinationFilteredRepos = destinationRepos
	}*/
	for _, sourceRepo := range sourceFilteredRepos {
		sourceTags, err := listTags(sourceRegistry, sourceRepo, sourceToken)
		if err != nil {
			panic(err)
		}
		/*repoFound := false
		for _, destinationRepo := range destinationFilteredRepos {
			if sourceRepo == destinationRepo {
				repoFound = true
				break
			}
		}*/
		for _, sourceTag := range sourceTags {
			image := ImageToReplicate{
				SourceRegistry:      sourceRegistry,
				SourceImage:         sourceRepo,
				DestinationRegistry: destinationRegistry,
				DestinationImage:    sourceRepo,
				SourceTag:           sourceTag,
				DestinationTag:      sourceTag,
			}
			err := replicate(image, creds)
			if err != nil {
				panic(err)
			}
			/*if !repoFound {
				err := replicate(image, creds)
				if err != nil {
					panic(err)
				}
			} else {
				destinationTagFound := false
				destinationTags, err := listTags(destinationRegistry, sourceRepo, destinationToken)
				if err != nil {
					panic(err)
				}
				for _, destinationTag := range destinationTags {
					if sourceTag == destinationTag {
						destinationTagFound = true
						break
					}
				}
				if destinationTagFound {
					continue
				} else {
					err = replicate(image, creds)
					if err != nil {
						panic(err)
					}
				}
			}*/
		}
	}
}
