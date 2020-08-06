package artifactory

import "log"

func ListFilesRecursive(host string, dir string, user string, pass string) (map[string]bool, error) {
	log.Println("ListRecursive: " + dir)
	outputFiles := make(map[string]bool)
	files, err := ListFiles(host, dir, user, pass)
	if err != nil {
		return nil, err
	}
	for fileName, isDir := range files {
		if isDir {
			log.Println("ListFiles: " + fileName)
			filesTemp, err := ListFiles(host, fileName, user, pass)
			if err != nil {
				return nil, err
			}
			for fileTemp, fileTempIsDir := range filesTemp {
				files[fileTemp] = fileTempIsDir
			}
		} else {
			outputFiles[fileName] = isDir
		}
	}
	return outputFiles, nil
}
