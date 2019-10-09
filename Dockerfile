FROM golang:latest AS builder
ADD ./replicate.go /app/
WORKDIR /app
RUN go get github.com/aws/aws-sdk-go/aws
RUN go get github.com/docker/docker/client
RUN go get github.com/docker/docker/api/types
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -o ./replicate ./replicate.go
# final stage
FROM alpine:latest
RUN apk --no-cache add ca-certificates
COPY --from=builder /app/replicate /
RUN chmod +x /replicate
ENTRYPOINT ["/replicate"]