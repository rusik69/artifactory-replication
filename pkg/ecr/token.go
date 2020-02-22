package ecr

import (
	"encoding/base64"
	"log"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ecr"
)

func GetToken() (string, string, error) {
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
