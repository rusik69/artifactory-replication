package helm

import (
	"io/ioutil"
	"log"
	"strings"

	"k8s.io/helm/pkg/repo"
)

func regenerateIndexYaml(artifactsList []string, artifactsListProd []string, sourceRepoUrl string, destinationRepoUrl string, sourceRepo string, prodRepo string, helmCdnDomain string) error {
	log.Println("Regenarating index.yamls")
	files := make(map[string]string)
	replicatedArtifacts := append(artifactsList, artifactsListProd...)
	for _, fileName := range replicatedArtifacts {
		if strings.Contains(fileName, "helm") {
			s := strings.Split(fileName, "/")
			filePrefix := strings.Join(s[1:len(s)-1], "/")
			fileRepo := s[0]
			files[filePrefix] = fileRepo
		}
	}
	for filePrefix, fileRepo := range files {
		sourceFileLocalPath, err := downloadFromArtifactory("https://"+sourceRepoUrl+"/artifactory/"+fileRepo+"/"+filePrefix+"/index.yaml", helmCdnDomain)
		if err != nil {
			return err
		}
		var sourceFileLocalPath2 string
		if fileRepo == sourceRepo {
			sourceFileLocalPath2, err = downloadFromArtifactory("https://"+sourceRepoUrl+"/artifactory/"+prodRepo+"/"+filePrefix+"/index.yaml", helmCdnDomain)
			if err != nil {
				return err
			}
		} else if fileRepo == prodRepo {
			sourceFileLocalPath2, err = downloadFromArtifactory("https://"+sourceRepoUrl+"/artifactory/"+sourceRepo+"/"+filePrefix+"/index.yaml", helmCdnDomain)
			if err != nil {
				return err
			}
		}
		sourceIndexFile, err := repo.LoadIndexFile(sourceFileLocalPath)
		if err != nil {
			return err
		}
		sourceIndexFile2, err := repo.LoadIndexFile(sourceFileLocalPath2)
		if err != nil {
			return err
		}
		sourceIndexFile.Merge(sourceIndexFile2)
		tempFile, err := ioutil.TempFile("", "index-yaml")
		if err != nil {
			return err
		}
		sourceIndexFile.WriteFile(tempFile.Name(), 0644)
		err = uploadToS3(destinationRepoUrl, filePrefix+"/index.yaml", tempFile.Name())
		if err != nil {
			return err
		}
	}
	return nil
}
