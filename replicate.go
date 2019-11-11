package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"regexp"
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
var failedDockerPullRepos, failedDockerPushRepos []list

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

func listArtifactoryFiles(host string, dir string, user string, pass string) (map[string]bool, error) {
	url := "https://" + host + "/artifactory/api/storage/" + dir
	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(user, pass)
	resp, err := client.Do(req)
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
		output[strings.Trim(file.Uri, "/")] = file.Folder
	}
	return output, nil
}

func listTags(dockerRegistry string, image string, user string, pass string) ([]string, error) {
	httpClient := &http.Client{}
	req, err := http.NewRequest("GET", "https://"+dockerRegistry+"/v2/"+image+"/tags/list", nil)
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
			log.Println(creds.DestinationUser)
			log.Println(creds.DestinationPassword)
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
	log.Println("Deleting " + imageName)
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
		return nil
	}
	err = deleteImage(sourceImage)
	if err != nil {
		log.Println(err)
		log.Println("Error deleting image, ignoring...")
		return nil
	}
	err = deleteImage(destinationImage)
	if err != nil {
		log.Println(err)
		log.Println("error deleting image, ignoring...")
		return nil
	}
	return nil
}

func replicateDocker(creds Creds, sourceRegistry string, destinationRegistry string, imageFilter string, destinationRegistryType string) {
	var copiedArtifacts uint = 0
	var reposLimit string
	if destinationRegistryType == "google" {
		reposLimit = "1000"
	} else {
		reposLimit = "1000000"
	}
	log.Println("Getting repos from source registry: " + sourceRegistry)
	sourceRepos, err := getRepos(sourceRegistry, creds.SourceUser, creds.SourcePassword, reposLimit)
	if err != nil {
		panic(err)
	}
	log.Println("Found source repos:")
	log.Println(sourceRepos)
	log.Println("Getting repos from destination from destination registry: " + destinationRegistry)
	destinationRepos, err := getRepos(destinationRegistry, creds.DestinationUser, creds.DestinationPassword, reposLimit)
	if err != nil {
		panic(err)
	}
	log.Println("Found destination repos:")
	log.Println(destinationRepos)
	dockerRepoPrefix := os.Getenv("DOCKER_REPO_PREFIX")
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
	for _, sourceRepo := range sourceFilteredRepos {
		sourceTags, err := listTags(sourceRegistry, sourceRepo, creds.SourceUser, creds.SourcePassword)
		if err != nil {
			panic(err)
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
		for _, sourceTag := range sourceTags {
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
					log.Println("Repo tag: " + sourceRepo + ":" + sourceTag + " not found")
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

func downloadFromArtifactory(fileUrl string, destinationRegistry string, helmCdnDomain string) string {
	log.Println("Downloading " + fileUrl)
	resp, err := http.Get(fileUrl)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	tempFile, err := ioutil.TempFile("", "artifactory-download")
	if err != nil {
		panic(err)
	}
	defer tempFile.Close()
	_, err = io.Copy(tempFile, resp.Body)
	if err != nil {
		panic(err)
	}
	matched, err := regexp.MatchString("/index.yaml$", fileUrl)
	if err != nil {
		panic(err)
	}
	if matched && helmCdnDomain != "" {
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			panic(err)
		}
		linkToReplace, err := regexp.Compile("(https?://.*?/artifactory/.*?/)")
		if err != nil {
			panic(err)
		}
		body = linkToReplace.ReplaceAll(body, []byte("https://"+helmCdnDomain+"/"))
		err = ioutil.WriteFile(tempFile.Name(), body, os.FileMode(0644))
		if err != nil {
			panic(err)
		} else {
			return tempFile.Name()
		}
	} else {
		return tempFile.Name()
	}
}

func uploadToS3(destinationRegistry string, destinationFileName string, tempFileName string) error {
	f, err := os.Open(tempFileName)
	if err != nil {
		return err
	}
	sess, err := session.NewSession(&aws.Config{})
	uploader := s3manager.NewUploader(sess, func(u *s3manager.Uploader) {
		u.PartSize = 5 * 1024 * 1024 // The minimum/default allowed part size is 5MB
		u.Concurrency = 2            // default is 5
	})
	log.Println("Uploading " + destinationFileName + " to " + destinationRegistry)
	_, err = uploader.Upload(&s3manager.UploadInput{
		Bucket: aws.String(destinationRegistry),
		Key:    aws.String(destinationFileName),
		Body:   f})
	return err
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
	var destinationBinariesList map[string]bool
	sourceBinariesList, err := listArtifactoryFiles(sourceRegistry, repo, creds.SourceUser, creds.SourcePassword)
	endpoint := os.Getenv("OSS_ENDPOINT")
	if endpoint == "" {
		endpoint = "oss-cn-beijing.aliyuncs.com"
	}
	if err != nil {
		panic(err)
	}
	if destinationRegistryType == "s3" {
		destinationBinariesList, err = ListS3Files(destinationRegistry)
		if err != nil {
			panic(err)
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
			replicateBinary(creds, sourceRegistry, destinationRegistry, destinationRegistryType, repo+"/"+fileName, helmCdnDomain)
		} else {
			fileUrl := "http://" + sourceRegistry + "/artifactory/" + repo + "/" + fileName
			fileFound := false
			for destinationFileName, _ := range destinationBinariesList {
				ss := strings.Split(destinationFileName, "/")
				destinationFileNameWithoutPath := ss[len(ss)-1]
				if destinationFileNameWithoutPath == fileName {
					log.Println("Found: " + destinationFileName)
					fileFound = true
					break
				}
			}
			if !fileFound || fileName == "index.yaml" {
				tempFileName := downloadFromArtifactory(fileUrl, destinationRegistry, helmCdnDomain)
				destinationFileName := repo + "/" + fileName
				destinationFileName = destinationFileName[strings.IndexByte(destinationFileName, '/'):]
				log.Println("Dest: " + destinationFileName)
				if destinationRegistryType == "s3" {
					err := uploadToS3(destinationRegistry, destinationFileName, tempFileName)
					if err != nil {
						panic(err)
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
				replicatedArtifacts += 1
				os.Remove(tempFileName)
			}
		}
	}
	log.Printf("%d artifacts copied to %s\n", replicatedArtifacts, repo)
}

func checkRepos(sourceRegistry string, destinationRegistry string, creds Creds, artifactType string, destinationRegistryType string) {
	log.Println("Checking " + destinationRegistryType + " repo consistency between " + sourceRegistry + " and " + destinationRegistry)
	var missingRepos []string
	var checkFailed bool
	if destinationRegistryType == "docker" {
		sourceRepos, err := getRepos(sourceRegistry, creds.SourceUser, creds.SourcePassword, "1000")
		if err != nil {
			panic(err)
		}
		destinationRepos, err := getRepos(destinationRegistry, creds.DestinationUser, creds.DestinationPassword, "1000")
		if err != nil {
			panic(err)
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
				break
			}
		}
		if checkFailed {
			log.Println("Consistency check failed, missing repos:")
			for _, missingRepo := range missingRepos {
				log.Println(missingRepo)
			}
			os.Exit(1)
		} else {
			log.Println("No missing repos found")
			return
		}
	}
}

func getAwsEcrToken() (string, string, error) {
	svc := ecr.New(session.New())
	input := &ecr.GetAuthorizationTokenInput{}
	result, err := svc.GetAuthorizationToken(input)
	if err != nil {
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
		checkRepos(sourceRegistry, destinationRegistry, creds, artifactType, destinationRegistryType)
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
	} else if artifactType == "binary" {
		if destinationRegistryType != "s3" && destinationRegistryType != "artifactory" && destinationRegistryType != "oss" {
			panic("unknown or empty DESTINATION_REGISTRY_TYPE")
		}
		log.Println("replicating binary repo " + imageFilter + " from " + sourceRegistry + " to " + destinationRegistry + " bucket")
		if helmCdnDomain != "" {
			log.Println("Helm CDN domain: " + helmCdnDomain)
		}
		replicateBinary(creds, sourceRegistry, destinationRegistry, destinationRegistryType, imageFilter, helmCdnDomain)
	} else {
		panic("unknown or empty ARTIFACT_TYPE")
	}
}
