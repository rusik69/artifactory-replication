package binary

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
)

func ComputeFileSHA256(filePath string) (string, error) {
	var returnSHA256 string
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	hashInBytes := hash.Sum(nil)
	returnSHA256 = hex.EncodeToString(hashInBytes)
	return returnSHA256, nil
}
