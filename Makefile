VERSION ?= 0.1.0
REGISTRY ?= safanaj
FLAGS =
ENVVAR = CGO_ENABLED=0
GOOS ?= $(shell go env GOOS) #linux
GO ?= go
LDFLAGS ?= -s -w
COMPONENT = dapr-cert-transformer

DOCKER_IMAGE = "$(REGISTRY)/$(COMPONENT):$(VERSION)"

SRCS = ./main.go

.PHONY: build static deps clean

golang:
	@echo "--> Go Version"
	@$(GO) version

deps:
	$(GO) mod tidy -v && $(GO) mod vendor -v

verify-deps:
	$(GO) mod verify && $(GO) mod tidy -v && $(GO) mod vendor -v

clean:
	rm -f ${COMPONENT}

clean-all: clean
	rm -rf vendor

build: golang
	@echo "--> Compiling the project"
	$(ENVVAR) GOOS=$(GOOS) $(GO) build -mod=vendor \
		-gcflags "-e" \
		-ldflags "$(LDFLAGS) -X main.version=${VERSION} -X main.progname=${COMPONENT}" \
		-v -o ${COMPONENT} $(SRCS)

static: golang
	@echo "--> Compiling the static binary"
	$(ENVVAR) GOARCH=amd64 GOOS=$(GOOS) $(GO) build -mod=vendor -a -tags netgo \
		-gcflags "-e" \
		-ldflags "$(LDFLAGS) -X main.version=${VERSION} -X main.progname=${COMPONENT}" \
		-v -o ${COMPONENT} $(SRCS)

# test:
# 	$(ENVVAR) GOOS=$(GOOS) $(GO) test -v ./...

docker: deps static docker-build

docker-build:
	docker build -t ${DOCKER_IMAGE} .

docker-push:
	docker image push $(DOCKER_IMAGE)

docker-build-in-docker:
	cat Dockerfile.build-in-docker.head > Dockerfile.build-in-docker
	cat Dockerfile | \
	    sed 's,ADD $(COMPONENT) /,COPY --from=builder /$(COMPONENT) /,' \
		>> Dockerfile.build-in-docker
	docker build -t ${DOCKER_IMAGE} -f Dockerfile.build-in-docker .
