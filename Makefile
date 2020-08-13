build:
	go build ./cmd/replicate/replicate.go
build_linux:
	GOOS=linux GOARCH=amd64 go build ./cmd/replicate/replicate.go
docker:
	docker build .