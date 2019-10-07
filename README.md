# artifactory-replication
ARTIFACTORY_TYPE: "binary" for binary artifacts replication, "docker" for docker images

# env variables for docker registry to docker registry replication:
SOURCE_REGISTRY: source docker registry to sync from

DESTINATION_REGISTRY: destination docker registry to sync to

IMAGE_FILTER: image path prefix

SOURCE_USER: source registry user, if needed

SOURCE_PASSWORD: source registry password, if needed

DESTINATION_USER: destination registry user, if needed

DESTINATION_PASSWORD: destination registry password, if needed


# env variables for artifactory binary to S3 replication
SOURCE_REGISTRY: source artifactory binary repo to sync from

DESTINATION_REGISTRY: destination S3 bucket name to sync to

IMAGE_FILTER: image path repository, recursive copy not supported, specify inmost directory

SOURCE_USER: source repository user, if needed

SOURCE_PASSWORD: source repository password, if needed

AWS_ACCESS_KEY_ID: aws access key id for user with write access to s3 bucket

AWS_SECRET_ACCESS_KEY: aws secret access key for user with write access to s3 bucket

AWS_REGION: aws region where bucket is located