FROM golang:latest AS builder
ADD . /app/
WORKDIR /app
RUN make
# final stage
FROM alpine:latest
RUN apk --no-cache add ca-certificates
COPY --from=builder /app/replicate /
RUN chmod +x /replicate
ENTRYPOINT ["/replicate"]