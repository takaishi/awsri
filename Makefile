COMMIT_HASH := $(shell git rev-parse --short HEAD)

build:
	go build -o dist/awsri -ldflags "-X main.version=$(COMMIT_HASH)" ./cmd/awsri

install:
	go install -ldflags "-X main.version=$(COMMIT_HASH)" github.com/takaishi/awsri/cmd/awsri