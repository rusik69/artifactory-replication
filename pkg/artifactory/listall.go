package artifactory

import (
	"log"
	"strings"
)

func ListAllFiles(host string, dir string, user string, pass string) ([]string, error) {
	log.Println("ListAllFiles: " + dir)
	var outputFiles, outputDirs []string
	files, err := ListFiles(host, dir, user, pass)
	if err != nil {
		return nil, err
	}
	for fileName, isDir := range files {
		if isDir == true {
			outputDirs = append(outputDirs, dir+"/"+fileName)
		} else {
			outputFiles = append(outputFiles, dir+"/"+fileName)
		}
	}
	for len(outputDirs) > 0 {
		fileNameWithDir := outputDirs[0]
		//log.Println("listFiles: " + fileNameWithDir)
		files, err := ListFiles(host, fileNameWithDir, user, pass)
		if err != nil {
			return nil, err
		}
		for fileName, isDir := range files {
			if !strings.HasPrefix(fileName, "/") {
				fileName = "/" + fileName
			}
			if !strings.HasPrefix(fileName, "/"+dir) {
				fileName = dir + fileName
			}
			//log.Println("fileName: " + fileName)
			if isDir == true {
				//log.Println("Is Directory")
				outputDirs = append(outputDirs, fileName)
			} else {
				//log.Println("Is File")
				outputFiles = append(outputFiles, fileName)
			}
		}
		outputDirs = outputDirs[1:]
	}
	var outputFilesStripped []string
	for _, file := range outputFiles {
		fileStripped := strings.TrimPrefix(file, dir+"/")
		outputFilesStripped = append(outputFilesStripped, fileStripped)
	}
	return outputFilesStripped, nil
}
