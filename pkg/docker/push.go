package docker

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/loqutus/artifactory-replication/pkg/credentials"
)

func pushImage(image ImageToReplicate, creds credentials.Creds) error {
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
		var failed bool
		backOffTime := backOffStart
		var out io.ReadCloser
		for i := 1; i <= backOffSteps; i++ {
			out, err := cli.ImagePush(ctx, destinationImage, types.ImagePushOptions{RegistryAuth: authStr})
			defer out.Close()
			if err != nil {
				failed = true
				log.Print("error pushing image", sourceImage, "retry", string(i))
				if i != backOffSteps {
					time.Sleep(time.Duration(backOffTime) * time.Millisecond)
				}
				backOffTime *= i
			} else {
				failed = false
				break
			}
		}
		if failed == true {
			return err
		}
		buf := new(bytes.Buffer)
		buf.ReadFrom(out)
		newStr := buf.String()
		if strings.Contains(newStr, "error") || strings.Contains(newStr, "Error") {
			return errors.New(newStr)
		}
	} else {
		var failed bool
		backOffTime := backOffStart
		var out io.ReadCloser
		for i := 1; i <= backOffSteps; i++ {
			out, err = cli.ImagePush(ctx, destinationImage, types.ImagePushOptions{})
			defer out.Close()
			if err != nil {
				failed = true
				log.Print("error pushing image", sourceImage, "retry", string(i))
				if i != backOffSteps {
					time.Sleep(time.Duration(backOffTime) * time.Millisecond)
				}
				backOffTime *= i
			} else {
				failed = false
				break
			}
		}
		if failed == true {
			return err
		}
		buf := new(bytes.Buffer)
		buf.ReadFrom(out)
		newStr := buf.String()
		if strings.Contains(newStr, "error") || strings.Contains(newStr, "Error") {
			return errors.New(newStr)
		}
	}
	return nil
}
