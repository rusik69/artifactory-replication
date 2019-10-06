FROM golang:latest AS builder
ADD ./replicate.go /app/
WORKDIR /app
RUN go get ./...
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -o /replicate ./replicate.go

# final stage
FROM alpine:latest
RUN apk --no-cache add ca-certificates
COPY --from=builder /app/replicate /
RUN chmod +x /replicate
ENTRYPOINT ["/replicate"]