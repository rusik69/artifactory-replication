package docker

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"io/ioutil"
	"log"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/loqutus/artifactory-replication/pkg/credentials"
)

func pullImage(image ImageToReplicate, creds credentials.Creds) error {
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
		var failed bool
		backOffTime := backOffStart
		var out io.ReadCloser
		for i := 1; i <= backOffSteps; i++ {
			out, err = cli.ImagePull(ctx, sourceImage, types.ImagePullOptions{RegistryAuth: authStr})
			defer out.Close()
			if err != nil {
				failed = true
				log.Print("error pulling image", sourceImage, "retry", string(i))
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
		io.Copy(ioutil.Discard, out)
	} else {
		var failed bool
		backOffTime := backOffStart
		var out io.ReadCloser
		for i := 1; i <= backOffSteps; i++ {
			out, err = cli.ImagePull(ctx, sourceImage, types.ImagePullOptions{})
			if err != nil {
				failed = true
				log.Print("error pulling image", sourceImage, "retry", string(i))
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
