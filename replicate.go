package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"regexp"
	"strings"
	"net"
	"golang.org/x/crypto/ssh"
	"github.com/aliyun/aliyun-oss-go-sdk/oss"
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

func GetRepos(dockerRegistry string, user string, pass string) ([]string, error) {
	client := &http.Client{}
	req, err := http.NewRequest("GET", "https://"+dockerRegistry+"/v2/_catalog?n=999999", nil)
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

func listTags(docker_registry string, image string, user string, pass string) ([]string, error) {
	httpClient := &http.Client{}
	req, err := http.NewRequest("GET", "https://"+docker_registry+"/v2/"+image+"/tags/list", nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(user, pass)
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
	fmt.Println("Pulling " + sourceImage)
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
	fmt.Println("Pushing " + destinationImage)
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
	fmt.Println("Deleting " + imageName)
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

func doReplicateDocker(image ImageToReplicate, creds Creds) error {
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

func replicateDocker(creds Creds, sourceRegistry string, destinationRegistry string, imageFilter string) {
	var copiedArtifacts uint = 0
	sourceRepos, err := GetRepos(sourceRegistry, creds.SourceUser, creds.SourcePassword)
	if err != nil {
		panic(err)
	}
	destinationRepos, err := GetRepos(destinationRegistry, creds.DestinationUser, creds.DestinationPassword)
	if err != nil {
		panic(err)
	}
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
			if sourceRepo == destinationRepo {
				repoFound = true
				fmt.Println("Found repo: " + sourceRepo)
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
				fmt.Println("Repo not found: " + sourceRepo)
				err := doReplicateDocker(image, creds)
				if err != nil {
					panic(err)
				}
				copiedArtifacts += 1
			} else {
				destinationTagFound := false
				destinationTags, err := listTags(destinationRegistry, sourceRepo, creds.DestinationUser, creds.DestinationPassword)
				if err != nil {
					panic(err)
				}
				for _, destinationTag := range destinationTags {
					if sourceTag == destinationTag {
						destinationTagFound = true
						fmt.Println("Found repo tag: " + sourceRepo + ":" + sourceTag)
						break
					}
				}
				if destinationTagFound {
					continue
				} else {
					fmt.Println("Not found image tag: " + sourceRepo + ":" + sourceTag)
					err := doReplicateDocker(image, creds)
					if err != nil {
						panic(err)
					}
					copiedArtifacts += 1
				}
			}
		}
	}
	fmt.Printf("%d artifacts copied\n", copiedArtifacts)
}

func ListS3Files(S3Bucket string) (map[string]bool, error) {
	sess, _ := session.NewSession(&aws.Config{})
	svc := s3.New(sess)
	output := make(map[string]bool)
	err := svc.ListObjectsPages(&s3.ListObjectsInput{Bucket: &S3Bucket,},
		func(p *s3.ListObjectsOutput, last bool) (shouldContinue bool) {
			for _, obj := range p.Contents {
				output[*obj.Key] = false
			}
			return true
		})
	return output, err
}

func downloadFromArtifactory(fileUrl string, destinationRegistry string, helmCdnDomain string) io.Reader {
	fmt.Println("Downloading " + fileUrl)
	resp, err := http.Get(fileUrl)
	if err != nil {
		panic(err)
	}
	matched, err := regexp.MatchString("/index.yaml$", fileUrl)
	if err != nil {
		panic(err)
	}
	var binaryToWrite io.Reader
	if matched && helmCdnDomain != "" {
		linkToReplace, err := regexp.Compile("(https?://.*?/artifactory/.*?/)")
		if err != nil {
			panic(err)
		}
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			panic(err)
		}
		var tmpBinaryToWrite bytes.Buffer
		tmpBinaryToWrite.Write(linkToReplace.ReplaceAll(body, []byte("https://"+helmCdnDomain+"/")))
		binaryToWrite = &tmpBinaryToWrite
	} else {
		binaryToWrite = resp.Body
	}
	return binaryToWrite
}

func uploadToS3(destinationRegistry string, destinationFileName string, body io.Reader) error {
	sess, err := session.NewSession(&aws.Config{})
	uploader := s3manager.NewUploader(sess)
	fmt.Println("Uploading " + destinationFileName + " to " + destinationRegistry)
	_, err = uploader.Upload(&s3manager.UploadInput{
		Bucket: aws.String(destinationRegistry),
		Key:    aws.String(destinationFileName),
		Body:   body})
	return err
}

func uploadToArtifactory(destinationRegistry string, repo string, destinationFileName string, destinationUser string, destinationPassword string, body io.Reader) error {
	url := "https://" + destinationRegistry + "/artifactory/" + repo + destinationFileName
	fmt.Println("Uploading: " + url)
	client := &http.Client{}
	req, err := http.NewRequest(http.MethodPut, url, body)
	if err != nil {
		return err
	}
	req.SetBasicAuth(destinationUser, destinationPassword)
	_, err = client.Do(req)
	return err
}

func listOssFiles(repo string, creds Creds) (map[string]bool, error){
	output := make(map[string]bool)
	endpoint := os.Getenv("OSS_ENDPOINT")
	if endpoint == ""{
		endpoint = "oss-cn-beijing.aliyuncs.com"
	}
	ossClient, err := oss.New(endpoint, creds.DestinationUser, creds.DestinationPassword)
	if err != nil {
		return output,err
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
	return output, err
}

func uploadToOss(destinationRegistry string, fileName string, creds Creds, jumpHostLocalPort string, body io.Reader) error{
	endpoint := os.Getenv("OSS_ENDPOINT")
	fmt.Println("Uploading " + fileName + " to " + destinationRegistry)
	if endpoint == ""{
		endpoint = "oss-cn-beijing.aliyuncs.com"
	}
	ossClient, err := oss.New(endpoint, creds.DestinationUser, creds.DestinationPassword)
	if err != nil {
		return err
	}
	bucket, err := ossClient.Bucket(destinationRegistry)
	if err != nil {
		return err
	}
	err = bucket.PutObject(fileName, body)
	return err
}

func replicateBinary(creds Creds, sourceRegistry string, destinationRegistry string, destinationRegistryType string, repo string, helmCdnDomain string, jumpHostLocalPort string) {
	fmt.Println("Processing repo " + repo)
	var replicatedArtifacts uint = 0
	var destinationBinariesList map[string]bool
	sourceBinariesList, err := listArtifactoryFiles(sourceRegistry, repo, creds.SourceUser, creds.SourcePassword)
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
		destinationBinariesList, err = listOssFiles(destinationRegistry, creds)
		if err != nil {
			panic(err)
		}
	}
	for fileName, fileIsDir := range sourceBinariesList {
		if fileIsDir {
			replicateBinary(creds, sourceRegistry, destinationRegistry, destinationRegistryType, repo+"/"+fileName, helmCdnDomain, "")
		} else {
			fileUrl := "http://" + sourceRegistry + "/artifactory/" + repo + "/" + fileName
			fileFound := false
			for destinationFileName, _ := range destinationBinariesList {
				ss := strings.Split(destinationFileName, "/")
				destinationFileNameWithoutPath := ss[len(ss)-1]
				if destinationFileNameWithoutPath == fileName {
					fmt.Println("Found: " + destinationFileName)
					fileFound = true
					break
				}
			}
			if ! fileFound || fileName == "index.yaml" {
				body := downloadFromArtifactory(fileUrl, destinationRegistry, helmCdnDomain)
				destinationFileName := repo + "/" + fileName
				destinationFileName = destinationFileName[strings.IndexByte(destinationFileName, '/'):]
				fmt.Println("Dest: " + destinationFileName)
				if destinationRegistryType == "s3" {
					err := uploadToS3(destinationRegistry, destinationFileName, body)
					if err != nil {
						panic(err)
					}
				} else if destinationRegistryType == "artifactory" {
					err := uploadToArtifactory(destinationRegistry, repo, fileName, creds.DestinationUser, creds.DestinationPassword, body)
					if err != nil {
						panic(err)
					}
				} else if destinationRegistryType == "oss"{
					ss := strings.Split(fileName, "/")
					fileNameWithoutRootSlash := strings.Join(ss[1:], "/")
					err := uploadToOss(destinationRegistry, fileNameWithoutRootSlash, creds, jumpHostLocalPort, body)
					if err != nil{
						panic(err)
					}
				}
				replicatedArtifacts += 1
			}
		}
	}
	fmt.Printf("%d artifacts copied\n", replicatedArtifacts)
}

func handleClient(client net.Conn, remote net.Conn) {
	defer client.Close()
	chDone := make(chan bool)

	// Start remote -> local data transfer
	go func() {
		_, err := io.Copy(client, remote)
		if err != nil {
			panic(err)
		}
		chDone <- true
	}()

	// Start local -> remote data transfer
	go func() {
		_, err := io.Copy(remote, client)
		if err != nil {
			panic(err)
		}
		chDone <- true
	}()

	<-chDone
}

func setupJumpHost(jumpHostName string, jumpHostUser string, jumpHostKey string, jumpHostDestination string, jumpHostLocalPort string){
	privateKeyContent, err := ioutil.ReadFile(jumpHostKey)
	if err != nil {
		panic(err)
	}
	privateKey, err := ssh.ParsePrivateKey(privateKeyContent)
	if err != nil{
		panic(err)
	}
	config := ssh.ClientConfig{
		User: jumpHostUser,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(privateKey),
		},
	}
	conn, err := ssh.Dial("tcp", jumpHostName, &config)
	if err != nil {
		panic(err)
	}
	defer conn.Close()
	remote, err := conn.Dial("tcp", jumpHostDestination)
	if err != nil {
		panic(err)
	}
	defer remote.Close()
	local, err := net.Listen("tcp", "127.0.0.1:"+jumpHostLocalPort)
	if err != nil {
		panic(err)
	}
	defer local.Close()
	for {
		client, err := local.Accept()
		if err != nil {
			panic(err)
		}
		handleClient(client, remote)
	}
	
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
	imageFilter := os.Getenv("IMAGE_FILTER")
	artifactType := os.Getenv("ARTIFACT_TYPE")
	destinationRegistryType := os.Getenv("DESTINATION_REGISTRY_TYPE")
	helmCdnDomain := os.Getenv("HELM_CDN_DOMAIN")
	jumpHostName := os.Getenv("JUMP_HOST_NAME")
	jumpHostUser := os.Getenv("JUMP_HOST_USER")
	jumpHostKey := os.Getenv("JUMP_HOST_KEY")
	jumpHostDestination := os.Getenv("JUMP_HOST_DESTINATION")
	jumpHostLocalPort := os.Getenv("JUMP_HOST_LOCAL_PORT")
	if jumpHostName != ""{
		go setupJumpHost(jumpHostName, jumpHostUser, jumpHostKey, jumpHostDestination, jumpHostLocalPort)
	}
	creds := Creds{
		SourceUser:          os.Getenv("SOURCE_USER"),
		SourcePassword:      os.Getenv("SOURCE_PASSWORD"),
		DestinationUser:     os.Getenv("DESTINATION_USER"),
		DestinationPassword: os.Getenv("DESTINATION_PASSWORD"),
	}

	if artifactType == "docker" {
		fmt.Println("Replicating docker images repo " + imageFilter + " from " + sourceRegistry + " to " + destinationRegistry)
		replicateDocker(creds, sourceRegistry, destinationRegistry, imageFilter)
	} else if artifactType == "binary" {
		if destinationRegistryType != "s3" && destinationRegistryType != "artifactory" && destinationRegistryType != "oss"{
			panic("unknown or empty DESTINATION_REGISTRY_TYPE")
		}
		fmt.Println("replicating binaries repo " + imageFilter + " from " + sourceRegistry + " to " + destinationRegistry + " S3 bucket")
		if helmCdnDomain != "" {
			fmt.Println("Helm CDN domain: " + helmCdnDomain)
		}
		replicateBinary(creds, sourceRegistry, destinationRegistry, destinationRegistryType, imageFilter, helmCdnDomain, jumpHostLocalPort)
	} else {
		panic("unknown or empty ARTIFACT_TYPE")
	}
}
