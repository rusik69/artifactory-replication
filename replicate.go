package main

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ecr"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
)

var ossProxyRunning bool
var failedDockerPullRepos, failedDockerPushRepos, failedDockerCleanRepos, failedArtifactoryDownload, failedS3Upload []string
var destinationBinariesList map[string]bool
var checkFailed bool
var checkFailedList []string
var missingRepos, missingRepoTags []string
var removedTags, skippedTags uint64

// Creds source/destination credentials
type Creds struct {
	SourceUser          string
	SourcePassword      string
	DestinationUser     string
	DestinationPassword string
}

// ImageToReplicate source/desination image parameters
type ImageToReplicate struct {
	SourceRegistry      string
	SourceImage         string
	DestinationRegistry string
	DestinationImage    string
	SourceTag           string
	DestinationTag      string
}

func getRepos(dockerRegistry string, user string, pass string, reposLimit string) ([]string, error) {
	client := &http.Client{}
	req, err := http.NewRequest("GET", "https://"+dockerRegistry+"/v2/_catalog?n="+reposLimit, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(user, pass)
	resp, err := client.Do(req)
	defer resp.Body.Close()
	if err != nil {
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

func getArtifactoryFileMD5(host string, fileName string, user string, pass string) (string, error) {
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
		Checksums map[string]string `json:checksums`
	}
	var result storageInfo
	err = json.Unmarshal(body, &result)
	if err != nil {
		return "", err
	}
	return result.Checksums["md5"], nil
}

func listArtifactoryFiles(host string, dir string, user string, pass string) (map[string]bool, error) {
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
		Uri    string
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
		Uri          string
	}
	var result storageInfo
	err = json.Unmarshal(body, &result)
	if err != nil {
		return nil, err
	}
	var output = make(map[string]bool)
	for _, file := range result.Children {
		fileNameWithPath := strings.Trim(result.Path, "/") + file.Uri
		output[fileNameWithPath] = file.Folder
	}
	return output, nil
}

func getDockerCreateTime(dockerRegistry string, image string, tag string, user string, pass string) (string, error) {
	httpClient := &http.Client{}
	req, err := http.NewRequest("GET", "https://"+dockerRegistry+"/v2/"+image+"/manifests/"+tag, nil)
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(user, pass)
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
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
		return nil, err
	}
	return b.Tags, nil
}

func pullImage(image ImageToReplicate, creds Creds) error {
	sourceImage := image.SourceRegistry + "/" + image.SourceImage + ":" + image.SourceTag
	log.Println("Pulling " + sourceImage)
	ctx := context.Background()
	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		return err
	}
	defer cli.Close()
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
		defer out.Close()
		buf := new(bytes.Buffer)
		buf.ReadFrom(out)
		newStr := buf.String()
		if strings.Contains(newStr, "error") || strings.Contains(newStr, "Error") {
			return errors.New(newStr)
		}
		io.Copy(ioutil.Discard, out)
	} else {
		out, err := cli.ImagePull(ctx, sourceImage, types.ImagePullOptions{})
		if err != nil {
			return err
		}
		defer out.Close()
		buf := new(bytes.Buffer)
		buf.ReadFrom(out)
		newStr := buf.String()
		if strings.Contains(newStr, "error") || strings.Contains(newStr, "Error") {
			return errors.New(newStr)
		}
		io.Copy(ioutil.Discard, out)
	}
	return nil
}

func pushImage(image ImageToReplicate, creds Creds) error {
	destinationImage := image.DestinationRegistry + "/" + image.DestinationImage + ":" + image.DestinationTag
	log.Println("Pushing " + destinationImage)
	sourceImage := image.SourceRegistry + "/" + image.SourceImage + ":" + image.SourceTag
	ctx := context.Background()
	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		return err
	}
	defer cli.Close()
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
			log.Println(out)
			return err
		}
		defer out.Close()
		buf := new(bytes.Buffer)
		buf.ReadFrom(out)
		newStr := buf.String()
		if strings.Contains(newStr, "error") || strings.Contains(newStr, "Error") {
			return errors.New(newStr)
		}
	} else {
		out, err := cli.ImagePush(ctx, destinationImage, types.ImagePushOptions{})
		if err != nil {
			log.Println(out)
			return err
		}
		defer out.Close()
		buf := new(bytes.Buffer)
		buf.ReadFrom(out)
		newStr := buf.String()
		if strings.Contains(newStr, "error") || strings.Contains(newStr, "Error") {
			return errors.New(newStr)
		}
	}
	return nil
}

func deleteImage(imageName string) error {
	log.Println("Deleting local image:" + imageName)
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

func doReplicateDocker(image ImageToReplicate, creds Creds, destinationRegistryType string, repoFound *bool, dockerRepoPrefix string) error {
	err := pullImage(image, creds)
	if err != nil {
		log.Println(err)
		log.Println("Error pulling image, ignoring...")
		failedDockerPullRepos = append(failedDockerPullRepos, image.SourceImage+":"+image.SourceTag)
		return nil
	}
	if destinationRegistryType == "aws" && *repoFound == false {
		log.Println("Creating destination repo: " + image.DestinationImage)
		input := ecr.CreateRepositoryInput{
			RepositoryName: &image.DestinationImage,
		}
		sess, _ := session.NewSession(&aws.Config{})
		svc := ecr.New(sess)
		output, err := svc.CreateRepository(&input)
		if err != nil {
			if !strings.Contains(err.Error(), "already exists") {
				log.Println(output.String())
				return err
			}
		}
		*repoFound = true
	} else if destinationRegistryType == "alicloud" || destinationRegistryType == "google" {
		if dockerRepoPrefix != "" {
			image.DestinationImage = dockerRepoPrefix + "/" + image.DestinationImage
		}
	}
	destinationImage := image.DestinationRegistry + "/" + image.DestinationImage + ":" + image.DestinationTag
	sourceImage := image.SourceRegistry + "/" + image.SourceImage + ":" + image.SourceTag
	err = pushImage(image, creds)
	if err != nil {
		log.Println(err)
		log.Println("Error pushing image, ignoring...")
		failedDockerPushRepos = append(failedDockerPushRepos, image.DestinationImage+":"+image.DestinationTag)
		return nil
	}
	err = deleteImage(sourceImage)
	if err != nil {
		log.Println(err)
		log.Println("Error deleting local image, ignoring...")
		failedDockerCleanRepos = append(failedDockerCleanRepos, image.SourceImage+":"+image.SourceTag)
		return nil
	}
	err = deleteImage(destinationImage)
	if err != nil {
		log.Println(err)
		log.Println("error deleting local image, ignoring...")
		failedDockerCleanRepos = append(failedDockerCleanRepos, image.DestinationImage+":"+image.DestinationTag)
		return nil
	}
	return nil
}

func getAzureDockerTagManifestDigest(registry string, image string, tag string, user string, pass string) (string, error) {
	log.Println("Getting tag manifest digest:", image+":"+tag)
	url := "https://" + registry + "/acr/v1/" + image + "/_manifests"
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
	if strings.Contains(string([]byte(body)), "error") || strings.Contains(string([]byte(body)), "Error") {
		return "", errors.New(string([]byte(body)))
	}
	r, err := regexp.Compile("sha256:(.+?),")
	if err != nil {
		return "", err
	}
	digestRaw := r.Find(body)
	if string(digestRaw) == "" {
		return "", nil
	}
	digestSha := digestRaw[0 : len(digestRaw)-2]
	//digest := digestSha[7:]
	digest := digestSha
	return string(digest), nil
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
			skippedTags += 1
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
				skippedTags += 1
				return nil
			}
		} else {
			log.Println("Tag", image+":"+tag, "have empty digest, skipping...")
			skippedTags += 1
			return nil
		}
	} else {
		log.Println("Unknown destination registry type:", destinationRegistryType)
		return errors.New("unknown destination registry type")
	}
	log.Println("Removed tag:", registry+"/"+image+":"+tag)
	removedTags += 1
	return nil
}

func dockerClean(reposLimit string, sourceFilteredRepos []string, destinationFilteredRepos []string, imageFilter string, destinationRegistry string, creds Creds, destinationRegistryType string) {
	log.Println("Cleaning repo:", destinationRegistry)
	sourceProdRegistry := os.Getenv("SOURCE_PROD_REGISTRY")
	if sourceProdRegistry == "" {
		panic("empty SOURCE_PROD_REGISTRY")
	}
	log.Println("Getting repos from prod source registry: " + sourceProdRegistry)
	prodSourceRegistryUser := os.Getenv("SOURCE_PROD_REGISTRY_USER")
	prodSourceRegistryPassword := os.Getenv("SOURCE_PROD_REGISTRY_PASSWORD")
	dockerCleanKeepTagsString := os.Getenv("DOCKER_CLEAN_KEEP_TAGS")
	if dockerCleanKeepTagsString == "" {
		dockerCleanKeepTagsString = "10"
	}
	log.Println("I'm going to remove last", dockerCleanKeepTagsString, "tags")
	dockerCleanKeepTags, err := strconv.Atoi(dockerCleanKeepTagsString)
	if err != nil {
		panic(err)
	}
	sourceProdRepos, err := getRepos(sourceProdRegistry, prodSourceRegistryUser, prodSourceRegistryPassword, reposLimit)
	if err != nil {
		panic(err)
	}
	var prodSourceFilteredRepos []string
	if imageFilter != "" {
		for _, repo := range sourceProdRepos {
			if strings.HasPrefix(repo, imageFilter) {
				prodSourceFilteredRepos = append(prodSourceFilteredRepos, repo)
			}
		}
	} else {
		sourceFilteredRepos = sourceProdRepos
	}
	log.Println("Found prod source repos: ", len(sourceProdRepos))
	for _, destinationRepo := range destinationFilteredRepos {
		log.Println("Processing destination repo:", destinationRepo)
		var filteredDestinationTags []string
		var repoProdFound bool
		for _, prodRepo := range sourceProdRepos {
			if prodRepo == destinationRepo {
				repoProdFound = true
				break
			}
		}
		destinationRepoTags, err := listTags(destinationRegistry, destinationRepo, creds.DestinationUser, creds.DestinationPassword)
		if err != nil {
			panic(err)
		}
		if repoProdFound {
			sourceProdRepoTags, err := listTags(sourceProdRegistry, destinationRepo, prodSourceRegistryUser, prodSourceRegistryPassword)
			if err != nil {
				panic(err)
			}
			for _, destinationTag := range destinationRepoTags {
				var tagFound bool
				for _, sourceProdTag := range sourceProdRepoTags {
					if destinationTag == sourceProdTag {
						log.Println("Found tag on prod source and destination:", destinationRepo+":"+destinationTag)
						tagFound = true
						break
					}
				}
				if !tagFound {
					filteredDestinationTags = append(filteredDestinationTags, destinationTag)
				}
			}
		} else {
			filteredDestinationTags = destinationRepoTags
		}
		timeTags := make(map[string]string)
		var values []string
		for _, destinationTag := range filteredDestinationTags {
			tagUploadDate, err := getDockerCreateTime(destinationRegistry, destinationRepo, destinationTag, creds.DestinationUser, creds.DestinationPassword)
			if err != nil {
				panic(err)
			}
			log.Println("Getting tag creation time:", destinationRegistry+"/"+destinationRepo+":"+destinationTag, tagUploadDate)
			timeTags[destinationTag] = tagUploadDate
			values = append(values, tagUploadDate)
		}
		sort.Strings(values)
		if len(values) > dockerCleanKeepTags {
			log.Println("Removing last", len(values)-dockerCleanKeepTags, "tags from:", destinationRegistry+"/"+destinationRepo)
			for i := len(values) - 1; i >= dockerCleanKeepTags; i-- {
				var tagToRemove string
				for k, v := range timeTags {
					if values[i] == v {
						tagToRemove = k
					}
				}
				err := dockerRemoveTag(destinationRegistry, destinationRepo, tagToRemove, destinationRegistryType, creds.DestinationUser, creds.DestinationPassword)
				if err != nil {
					panic(err)
				}
			}
		}
	}
	log.Println("Removed", removedTags, "tags")
	log.Println("Skipped", skippedTags, "tags")
}

func replicateDocker(creds Creds, sourceRegistry string, destinationRegistry string, imageFilter string, destinationRegistryType string) {
	var copiedArtifacts uint = 0
	var reposLimit string
	if destinationRegistryType == "aws" {
		reposLimit = "1000"
	} else {
		reposLimit = "1000000"
	}
	log.Println("Getting repos from source registry: " + sourceRegistry)
	sourceRepos, err := getRepos(sourceRegistry, creds.SourceUser, creds.SourcePassword, reposLimit)
	if err != nil {
		panic(err)
	}
	log.Println("Found source repos: ", len(sourceRepos))
	log.Println("Getting repos from destination from destination registry: " + destinationRegistry)
	destinationRepos, err := getRepos(destinationRegistry, creds.DestinationUser, creds.DestinationPassword, reposLimit)
	if err != nil {
		panic(err)
	}
	log.Println("Found destination repos: ", len(destinationRepos))
	dockerRepoPrefix := os.Getenv("DOCKER_REPO_PREFIX")
	dockerTag := os.Getenv("DOCKER_TAG")
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
	log.Println("Found filtered source repos: ", len(sourceFilteredRepos))
	destinationFilteredRepos := destinationRepos[:0]
	if imageFilter != "" {
		for _, repo := range destinationRepos {
			if strings.HasPrefix(repo, imageFilter) {
				destinationFilteredRepos = append(destinationFilteredRepos, repo)
			}
		}
	} else {
		destinationFilteredRepos = destinationRepos
	}
	log.Println("Found filtered destination repos: ", len(destinationFilteredRepos))
	dockerCleanup := os.Getenv("DOCKER_CLEAN")
	if dockerCleanup == "true" {
		dockerClean(reposLimit, sourceFilteredRepos, destinationFilteredRepos, imageFilter, destinationRegistry, creds, destinationRegistryType)
		return
	}
	for _, sourceRepo := range sourceFilteredRepos {
		sourceTags, err := listTags(sourceRegistry, sourceRepo, creds.SourceUser, creds.SourcePassword)
		if err != nil {
			panic(err)
		}
		var sourceTagsFiltered []string
		if dockerTag != "" {
			for _, sourceTag := range sourceTags {
				if sourceTag == dockerTag {
					sourceTagsFiltered = append(sourceTagsFiltered, sourceTag)
				}
			}
		} else {
			sourceTagsFiltered = sourceTags
		}
		repoFound := false
		for _, destinationRepo := range destinationFilteredRepos {
			if destinationRegistryType == "google" {
				destinationRepo = strings.TrimPrefix(destinationRepo, dockerRepoPrefix+"/")
			}
			if sourceRepo == destinationRepo {
				repoFound = true
				log.Println("Found repo: " + sourceRepo)
				break
			}
		}
		for _, sourceTag := range sourceTagsFiltered {
			image := ImageToReplicate{
				SourceRegistry:      sourceRegistry,
				SourceImage:         sourceRepo,
				DestinationRegistry: destinationRegistry,
				DestinationImage:    sourceRepo,
				SourceTag:           sourceTag,
				DestinationTag:      sourceTag,
			}
			if !repoFound {
				log.Println("Destination repo not found: " + sourceRepo)
				err := doReplicateDocker(image, creds, destinationRegistryType, &repoFound, dockerRepoPrefix)
				if err != nil {
					panic(err)
				}
				copiedArtifacts += 1
			} else {
				destinationTagFound := false
				destinationRepo := sourceRepo
				if dockerRepoPrefix != "" {
					destinationRepo = dockerRepoPrefix + "/" + sourceRepo
				}
				destinationTags, err := listTags(destinationRegistry, destinationRepo, creds.DestinationUser, creds.DestinationPassword)
				if err != nil {
					panic(err)
				}
				for _, destinationTag := range destinationTags {
					if sourceTag == destinationTag {
						destinationTagFound = true
						log.Println("Found repo tag: " + sourceRepo + ":" + sourceTag)
						break
					}
				}
				if destinationTagFound {
					continue
				} else {
					log.Println("Repo tag: " + sourceRepo + ":" + sourceTag + " not found at destination, replicating...")
					err := doReplicateDocker(image, creds, destinationRegistryType, &repoFound, dockerRepoPrefix)
					if err != nil {
						panic(err)
					}
					copiedArtifacts += 1
				}
			}
		}
	}
	log.Printf("%d artifacts copied\n", copiedArtifacts)
}

func ListS3Files(S3Bucket string) (map[string]bool, error) {
	sess, _ := session.NewSession(&aws.Config{})
	svc := s3.New(sess)
	output := make(map[string]bool)
	err := svc.ListObjectsPages(&s3.ListObjectsInput{Bucket: &S3Bucket},
		func(p *s3.ListObjectsOutput, last bool) (shouldContinue bool) {
			for _, obj := range p.Contents {
				output[*obj.Key] = false
			}
			return true
		})
	return output, err
}

func getS3FileMD5(S3Bucket string, filename string) (string, error) {
	sess, _ := session.NewSession(&aws.Config{})
	svc := s3.New(sess)
	input := &s3.HeadObjectInput{
		Bucket: aws.String(S3Bucket),
		Key:    aws.String(filename),
	}
	object, err := svc.HeadObject(input)
	if err != nil {
		return "", err
	}
	if val, ok := object.Metadata["md5"]; ok {
		return *val, nil
	} else {
		return "", errors.New("Missing md5 in metadata")
	}
}

func downloadFromArtifactory(fileUrl string, destinationRegistry string, helmCdnDomain string) (string, error) {
	log.Println("Downloading " + fileUrl)
	resp, err := http.Get(fileUrl)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	tempFile, err := ioutil.TempFile("", "artifactory-download")
	if err != nil {
		return "", err
	}
	if resp.StatusCode != 200 {
		return "", errors.New("Response code error: " + string(resp.StatusCode))
	}
	_, err = io.Copy(tempFile, resp.Body)
	if err != nil {
		return "", err
	}
	fileName := tempFile.Name()
	tempFile.Close()
	matched, err := regexp.MatchString("/index.yaml$", fileUrl)
	if err != nil {
		return "", err
	}
	if matched && helmCdnDomain != "" {
		body, err := ioutil.ReadFile(fileName)
		if err != nil {
			return "", err
		}
		linkToReplace, err := regexp.Compile("(https?://.*?artifactory.*?/(artifactory/)?[^/]*?/)")
		if err != nil {
			return "", err
		}
		log.Println("Rewriting index.yaml urls...")
		body = linkToReplace.ReplaceAll(body, []byte("https://"+helmCdnDomain+"/"))
		err = ioutil.WriteFile(fileName, body, os.FileMode(0644))
		if err != nil {
			return "", err
		} else {
			return fileName, nil
		}
	} else {
		return fileName, nil
	}
}

func uploadToS3(destinationRegistry string, destinationFileName string, tempFileName string) error {
	f, err := os.Open(tempFileName)
	if err != nil {
		return err
	}
	sess, err := session.NewSession(&aws.Config{})
	if err != nil {
		return err
	}
	uploader := s3manager.NewUploader(sess, func(u *s3manager.Uploader) {
		u.PartSize = 5 * 1024 * 1024 // The minimum/default allowed part size is 5MB
		u.Concurrency = 2            // default is 5
	})
	fileMD5, err := computeFileMD5(tempFileName)
	if err != nil {
		return err
	}
	log.Println("Uploading "+destinationFileName+" to "+destinationRegistry, "MD5:", fileMD5)
	_, err = uploader.Upload(&s3manager.UploadInput{
		Bucket: aws.String(destinationRegistry),
		Key:    aws.String(destinationFileName),
		Body:   f,
		Metadata: map[string]*string{
			"md5": aws.String(fileMD5),
		}})
	return err
}

func computeFileMD5(filePath string) (string, error) {
	var returnMD5String string
	file, err := os.Open(tempFileName)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := md5.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	hashInBytes := hash.Sum(nil)[:16]
	returnMD5String = hex.EncodeToString(hashInBytes)
	return returnMD5String, nil
}

func uploadToArtifactory(destinationRegistry string, repo string, destinationFileName string, destinationUser string, destinationPassword string, tempFileName string) error {
	url := "https://" + destinationRegistry + "/artifactory/" + repo + destinationFileName
	log.Println("Uploading: " + url)
	f, err := os.Open(tempFileName)
	if err != nil {
		return err
	}
	client := &http.Client{}
	req, err := http.NewRequest(http.MethodPut, url, f)
	if err != nil {
		return err
	}
	req.SetBasicAuth(destinationUser, destinationPassword)
	_, err = client.Do(req)
	return err
}

func listOssFiles(repo string, creds Creds, endpoint string) (map[string]bool, error) {
	output := make(map[string]bool)
	ossClient, err := oss.New(endpoint, creds.DestinationUser, creds.DestinationPassword)
	if err != nil {
		return output, err
	}
	bucket, err := ossClient.Bucket(repo)
	if err != nil {
		return output, err
	}
	lsRes, err := bucket.ListObjects()
	if err != nil {
		return output, err
	}
	for _, object := range lsRes.Objects {
		output[object.Key] = false
	}
	return output, nil
}

func uploadToOss(destinationRegistry string, fileName string, creds Creds, tempFileName string, endpoint string) error {
	log.Println("Uploading " + fileName + " to " + destinationRegistry)
	ossClient, err := oss.New(endpoint, creds.DestinationUser, creds.DestinationPassword)
	if err != nil {
		return err
	}
	bucket, err := ossClient.Bucket(destinationRegistry)
	if err != nil {
		return err
	}
	f, err := os.Open(tempFileName)
	if err != nil {
		return err
	}
	attempts := 5
	for i := 0; i < attempts; i += 1 {
		if i >= 1 {
			log.Println(err)
			log.Printf("Attempt: %d\n", i)
		}
		err = bucket.PutObject(fileName, f)
		if err == nil {
			break
		}
		time.Sleep(5 * time.Second)
	}
	return err
}

func replicateBinary(creds Creds, sourceRegistry string, destinationRegistry string, destinationRegistryType string, repo string, helmCdnDomain string) {
	log.Println("Processing repo " + repo)
	var replicatedArtifacts uint = 0
	sourceBinariesList, err := listArtifactoryFiles(sourceRegistry, repo, creds.SourceUser, creds.SourcePassword)
	if err != nil {
		panic(err)
	}
	log.Println("Found source binaries:", len(sourceBinariesList))
	endpoint := os.Getenv("OSS_ENDPOINT")
	if endpoint == "" {
		endpoint = "oss-cn-beijing.aliyuncs.com"
	}
	if destinationRegistryType == "s3" {
		if len(destinationBinariesList) == 0 {
			destinationBinariesList, err = ListS3Files(destinationRegistry)
			if err != nil {
				panic(err)
			}
			log.Println("Found destination binaries:", len(destinationBinariesList))
		}
	} else if destinationRegistryType == "artifactory" {
		destinationBinariesList, err = listArtifactoryFiles(destinationRegistry, repo, creds.DestinationUser, creds.DestinationPassword)
		if err != nil {
			panic(err)
		}
	} else if destinationRegistryType == "oss" {
		destinationBinariesList, err = listOssFiles(destinationRegistry, creds, endpoint)
		if err != nil {
			panic(err)
		}
	}
	for fileName, fileIsDir := range sourceBinariesList {
		if fileIsDir {
			log.Println("Processing source dir: " + fileName)
			fileNameSplit := strings.Split(fileName, "/")
			fileNameWithoutRepo := fileNameSplit[len(fileNameSplit)-1]
			replicateBinary(creds, sourceRegistry, destinationRegistry, destinationRegistryType, repo+"/"+fileNameWithoutRepo, helmCdnDomain)
		} else {
			fileNameSplit := strings.Split(fileName, "/")
			fileNameWithoutPath := fileNameSplit[len(fileNameSplit)-1]
			fileUrl := "http://" + sourceRegistry + "/artifactory/" + repo + "/" + fileNameWithoutPath
			fileFound := false
			for destinationFileName, _ := range destinationBinariesList {
				if destinationFileName == fileName {
					log.Println("Found binary in destination: " + destinationFileName)
					fileFound = true
					break
				}
			}
			if !fileFound || fileNameWithoutPath == "index.yaml" || fileNameWithoutPath == "index.yaml.sha256" || fileNameWithoutPath == "get_kaas.sh" {
				tempFileName, err := downloadFromArtifactory(fileUrl, destinationRegistry, helmCdnDomain)
				if err != nil {
					log.Println("downloadFromArtifactory failed:")
					log.Println(err)
					failedArtifactoryDownload = append(failedArtifactoryDownload, fileUrl)
					continue
				}
				repoWithoutPathSplit := strings.Split(repo, "/")
				repoWithoutPath := repoWithoutPathSplit[1]
				destinationFileName := repoWithoutPath + "/" + fileName
				destinationFileName = destinationFileName[strings.IndexByte(destinationFileName, '/'):]
				log.Println("Dest: " + destinationFileName)
				if destinationRegistryType == "s3" {
					err := uploadToS3(destinationRegistry, destinationFileName, tempFileName)
					if err != nil {
						log.Println("uploadToS3 failed:")
						log.Println(err)
						failedS3Upload = append(failedS3Upload, destinationFileName)
						continue
					}
				} else if destinationRegistryType == "artifactory" {
					err := uploadToArtifactory(destinationRegistry, repo, fileName, creds.DestinationUser, creds.DestinationPassword, tempFileName)
					if err != nil {
						panic(err)
					}
				} else if destinationRegistryType == "oss" {
					destinationFileName = strings.TrimPrefix(destinationFileName, "/")
					err := uploadToOss(destinationRegistry, destinationFileName, creds, tempFileName, endpoint)
					if err != nil {
						panic(err)
					}
				}
				replicatedArtifacts++
				os.Remove(tempFileName)
			}
		}
	}
	log.Printf("%d artifacts copied to %s\n", replicatedArtifacts, repo)
}

func checkDockerRepos(sourceRegistry string, destinationRegistry string, destinationRegistryType string, creds Creds) error {
	var reposLimit string
	if destinationRegistryType == "aws" {
		reposLimit = "1000"
	} else {
		reposLimit = "1000000"
	}
	log.Println("Getting source repos from: " + sourceRegistry)
	sourceRepos, err := getRepos(sourceRegistry, creds.SourceUser, creds.SourcePassword, "1000000")
	if err != nil {
		return err
	}
	log.Println("Getting destination repos from: " + destinationRegistry)
	destinationRepos, err := getRepos(destinationRegistry, creds.DestinationUser, creds.DestinationPassword, reposLimit)
	if err != nil {
		return err
	}
	for _, sourceRepo := range sourceRepos {
		var destinationRepoFound bool
		for _, destinationRepo := range destinationRepos {
			if sourceRepo == destinationRepo {
				log.Println("Repo " + sourceRepo + " found")
				destinationRepoFound = true
				break
			}
		}
		if !destinationRepoFound {
			log.Println("Repo " + sourceRepo + " NOT found")
			checkFailed = true
			missingRepos = append(missingRepos, sourceRepo)
		}
		if !checkFailed {
			sourceRepoTags, err := listTags(sourceRegistry, sourceRepo, creds.SourceUser, creds.SourcePassword)
			if err != nil {
				log.Println("Failed to get tags for repo: " + sourceRepo)
				missingRepos = append(missingRepos, sourceRepo)
				checkFailed = true
			}
			destinationRepoTags, err := listTags(destinationRegistry, sourceRepo, creds.DestinationUser, creds.DestinationPassword)
			if err != nil {
				log.Println("Failed to get tags for repo: " + sourceRepo)
				missingRepos = append(missingRepos, sourceRepo)
				checkFailed = true
			}
			for _, sourceRepoTag := range sourceRepoTags {
				tagFound := false
				for _, destinationRepoTag := range destinationRepoTags {
					if sourceRepoTag == destinationRepoTag {
						log.Println("Repo tag: " + sourceRepo + ":" + sourceRepoTag + " found")
						tagFound = true
						break
					}
				}
				if !tagFound {
					log.Println("Tag not found: " + sourceRepoTag)
					missingRepoTags = append(missingRepoTags, sourceRepo+":"+sourceRepoTag)
					checkFailed = true
				}
			}
		}
	}
	return nil
}

func checkBinaryRepos(sourceRegistry string, destinationRegistry string, destinationRegistryType string, creds Creds, dir string) error {
	log.Println("Getting source repos from: " + sourceRegistry)
	sourceFilesWithDirs, err := listArtifactoryFiles(sourceRegistry, dir, creds.SourceUser, creds.SourcePassword)
	if err != nil {
		log.Println("listArtifactorFiles failed")
		return err
	}
	if len(destinationBinariesList) == 0 {
		destinationBinariesList, err = ListS3Files(destinationRegistry)
		if err != nil {
			log.Println("listS3Files failed")
			return err
		}
	}
	for sourceFile, isDir := range sourceFilesWithDirs {
		if isDir {
			log.Println("Processing source dir: " + sourceFile)
			fileNameSplit := strings.Split(sourceFile, "/")
			fileNameWithoutRepo := fileNameSplit[len(fileNameSplit)-1]
			checkBinaryRepos(sourceRegistry, destinationRegistry, destinationRegistryType, creds, dir+"/"+fileNameWithoutRepo)
		} else {
			var found bool
			for destinationBinary, _ := range destinationBinariesList {
				if destinationBinary == sourceFile {
					log.Println("Found:", destinationBinary)
					found = true
					sourceMD5, err := getArtifactoryFileMD5(sourceRegistry, sourceFile, creds.SourceUser, creds.SourcePassword)
					if err != nil {
						log.Println("Error getting source file md5:", sourceFile)
						log.Println(err)
						checkFailed = true
						checkFailedList = append(checkFailedList, sourceFile)
						continue
					}
					destinationMD5, err := getS3FileMD5(destinationRegistry, destinationBinary)
					if err != nil {
						log.Println("Error getting destination file md5:", destinationBinary)
						log.Println(err)
						checkFailed = true
						checkFailedList = append(checkFailedList, destinationBinary)
						continue
					}
					if sourceMD5 != destinationMD5 {
						log.Println("MD5 mismatch:", destinationBinary)
						log.Println("Source MD5:", sourceMD5)
						log.Println("DestinationMD5:", destinationMD5)
						checkFailed = true
						checkFailedList = append(checkFailedList, destinationBinary)
						continue
					}
					break
				}
			}
			if !found {
				checkFailed = true
				log.Println("Not found:", sourceFile)
				checkFailedList = append(checkFailedList, sourceFile)
			}
		}
	}
	return nil
}

func checkRepos(sourceRegistry string, destinationRegistry string, creds Creds, artifactType string, destinationRegistryType string, dir string) {
	log.Println("Checking " + destinationRegistryType + " repo consistency between " + sourceRegistry + " and " + destinationRegistry)
	var slackMessage string
	if artifactType == "docker" {
		err := checkDockerRepos(sourceRegistry, destinationRegistry, destinationRegistryType, creds)
		if err != nil {
			err := sendSlackNotification(err.Error())
			if err != nil {
				panic(err)
			}
		}
		if checkFailed {
			if len(missingRepos) > 0 {
				log.Println("Consistency check failed, missing docker repos:")
				slackMessage += "Consistency check failed, missing docker repos:\n"
				for _, missingRepo := range missingRepos {
					log.Println(missingRepo)
					slackMessage += missingRepo + "\n"
				}
			}
			if len(missingRepoTags) > 0 {
				log.Println("Consistency check failed, missing docker tags:")
				slackMessage += "Consistency check failed, missing docker tags:\n"
				for _, missingRepoTag := range missingRepoTags {
					log.Println(missingRepoTag)
					slackMessage += missingRepoTag + "\n"
				}
			}
			err := sendSlackNotification(slackMessage)
			if err != nil {
				panic(err)
			}
			os.Exit(1)
		} else {
			log.Println("No missing repos found")
			return
		}
	} else if artifactType == "binary" {
		err := checkBinaryRepos(sourceRegistry, destinationRegistry, destinationRegistryType, creds, dir)
		if err != nil {
			err := sendSlackNotification(err.Error())
			if err != nil {
				panic(err)
			}
		}
		if checkFailed {
			log.Println("Repo check failed, files not found in destination:")
			log.Println(checkFailedList)
			slackMessage += "Repo check failed, files not found in destination:\n"
			for _, file := range checkFailedList {
				slackMessage += file + "\n"
			}
			err := sendSlackNotification(slackMessage)
			if err != nil {
				panic(err)
			}
			os.Exit(1)
		}
	}
}

func getAwsEcrToken() (string, string, error) {
	sess, err := session.NewSession(&aws.Config{})
	if err != nil {
		return "", "", err
	}
	svc := ecr.New(sess)
	input := &ecr.GetAuthorizationTokenInput{}
	result, err := svc.GetAuthorizationToken(input)
	if err != nil {
		log.Println("svc.GetAuthorizationToken error")
		return "", "", err
	}
	encodedToken := *result.AuthorizationData[0].AuthorizationToken
	decodedToken, err := base64.StdEncoding.DecodeString(encodedToken)
	if err != nil {
		return "", "", err
	}
	tokenSplit := strings.Split(string(decodedToken), ":")
	return tokenSplit[0], tokenSplit[1], nil
}

func sendSlackNotification(msg string) error {
	slackWebhook := os.Getenv("SLACK_WEBHOOK")
	channel := os.Getenv("SLACK_CHANNEL")
	user := os.Getenv("SLACK_USER")
	if slackWebhook != "" {
		log.Println("Sending slack notification...")
		type SlackRequestBody struct {
			Text    string `json:"text"`
			Channel string `json:"channel"`
			User    string `json:"username"`
		}
		slackBody, _ := json.Marshal(SlackRequestBody{Text: msg, Channel: channel, User: user})
		req, err := http.NewRequest(http.MethodPost, slackWebhook, bytes.NewBuffer(slackBody))
		if err != nil {
			return err
		}
		req.Header.Add("Content-Type", "application/json")
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		buf := new(bytes.Buffer)
		buf.ReadFrom(resp.Body)
		log.Println(buf.String())
		if buf.String() != "ok" {
			return errors.New("Non-ok response returned from Slack")
		}
	}
	return nil
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	sourceRegistry := os.Getenv("SOURCE_REGISTRY")
	if sourceRegistry == "" {
		panic("empty SOURCE_REGISTRY env variable")
	}
	destinationRegistry := os.Getenv("DESTINATION_REGISTRY")
	if destinationRegistry == "" {
		panic("empty DESTINATION_REGISTRY env variable")
	}
	imageFilter := os.Getenv("IMAGE_FILTER")
	artifactType := os.Getenv("ARTIFACT_TYPE")
	destinationRegistryType := os.Getenv("DESTINATION_REGISTRY_TYPE")
	helmCdnDomain := os.Getenv("HELM_CDN_DOMAIN")
	creds := Creds{
		SourceUser:          os.Getenv("SOURCE_USER"),
		SourcePassword:      os.Getenv("SOURCE_PASSWORD"),
		DestinationUser:     os.Getenv("DESTINATION_USER"),
		DestinationPassword: os.Getenv("DESTINATION_PASSWORD"),
	}
	checkReposFlag := os.Getenv("CHECK_REPOS")
	if checkReposFlag == "true" {
		if artifactType == "docker" || artifactType == "binary" {
			checkRepos(sourceRegistry, destinationRegistry, creds, artifactType, destinationRegistryType, imageFilter)
		} else {
			log.Println("unknown artifact type: ", artifactType)
			os.Exit(1)
		}
		os.Exit(0)
	}
	if artifactType == "docker" {
		if imageFilter != "" {
			log.Println("Replicating docker images repo " + imageFilter + " from " + sourceRegistry + " to " + destinationRegistry)
		} else {
			log.Println("Replicating docker images from " + sourceRegistry + " to " + destinationRegistry)
		}
		if destinationRegistryType == "aws" {
			currentAccessKey := os.Getenv("AWS_ACCESS_KEY_ID")
			if currentAccessKey == "" {
				os.Setenv("AWS_ACCESS_KEY_ID", creds.DestinationUser)
			}
			currentSecretKey := os.Getenv("AWS_SECRET_ACCESS_KEY")
			if currentSecretKey == "" {
				os.Setenv("AWS_SECRET_ACCESS_KEY", creds.DestinationPassword)
			}
			ECRLogin, ECRPassword, err := getAwsEcrToken()
			if err != nil {
				panic(err)
			}
			creds.DestinationUser = ECRLogin
			creds.DestinationPassword = ECRPassword
		}
		if destinationRegistryType != "azure" && destinationRegistryType != "aws" && destinationRegistryType != "alicloud" && destinationRegistryType != "google" {
			if destinationRegistryType == "" {
				destinationRegistryType = "azure"
			} else {
				panic("unknown DESTINATION_REGISTRY_TYPE")
			}
		}
		replicateDocker(creds, sourceRegistry, destinationRegistry, imageFilter, destinationRegistryType)
		if len(failedDockerPushRepos) != 0 || len(failedDockerPullRepos) != 0 || len(failedDockerCleanRepos) != 0 {
			log.Println("Failed docker operations:")
			if len(failedDockerPushRepos) != 0 {
				log.Println("Docker push failed:")
				log.Println(failedDockerPushRepos)
			}
			if len(failedDockerPullRepos) != 0 {
				log.Println("Docker pull failed:")
				log.Println(failedDockerPullRepos)
			}
			if len(failedDockerCleanRepos) != 0 {
				log.Println("Docker clean failed:")
				log.Println(failedDockerCleanRepos)
			}
			os.Exit(1)
		}
	} else if artifactType == "binary" {
		if destinationRegistryType != "s3" && destinationRegistryType != "artifactory" && destinationRegistryType != "oss" {
			panic("unknown or empty DESTINATION_REGISTRY_TYPE")
		}
		log.Println("replicating binary repo " + imageFilter + " from " + sourceRegistry + " to " + destinationRegistry + " bucket")
		if helmCdnDomain != "" {
			log.Println("Helm CDN domain: " + helmCdnDomain)
		}
		replicateBinary(creds, sourceRegistry, destinationRegistry, destinationRegistryType, imageFilter, helmCdnDomain)
		if len(failedArtifactoryDownload) != 0 || len(failedS3Upload) != 0 {
			if len(failedS3Upload) != 0 {
				log.Println("S3 upload failed:")
				log.Println(failedS3Upload)
			}
			if len(failedArtifactoryDownload) != 0 {
				log.Println("Failed artifactory download:")
				log.Println(failedArtifactoryDownload)
			}
			os.Exit(1)
		}
	} else {
		panic("unknown or empty ARTIFACT_TYPE")
	}
}
