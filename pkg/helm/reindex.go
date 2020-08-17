package helm

import (
	"log"
	"os"
	"strings"

	"github.com/loqutus/artifactory-replication/pkg/s3"
	"k8s.io/helm/pkg/repo"
)

func Reindex(filesList []string, registry string, allFiles []string, helmCdnDomain string) error {
	var filePrefixes = make(map[string]bool)
	for _, file := range filesList {
		if strings.Contains(file, "/helm/") {
			s := strings.Split(file, "/")
			filePrefix := strings.Join(s[:len(s)-1], "/")
			filePrefixes[filePrefix] = true
		}
	}
	for prefix, _ := range filePrefixes {
		log.Println("Reindexing", prefix)
		dir, err := s3.DownloadAllFiles(registry, prefix, allFiles)
		if err != nil {
			return err
		}
		defer os.RemoveAll(dir)
		indexFile, err := repo.IndexDirectory(dir, helmCdnDomain)
		if err != nil {
			return err
		}
		tempFileName := dir + "/index.yaml"
		err = indexFile.WriteFile(tempFileName, 0660)
		if err != nil {
			return err
		}
		log.Println("Written index file", tempFileName)
		log.Println("Uploading", prefix+"/index.yaml")
		err = s3.Upload(registry, prefix+"/index.yaml", tempFileName)
		if err != nil {
			return err
		}
	}
	return nil
}
