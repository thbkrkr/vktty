ORG 		= krkr
NAME 		= ktty
VERSION = $(shell git rev-parse --short HEAD)

all: build

build:
	docker buildx build --rm -t $(ORG)/$(NAME):$(VERSION) . --platform=linux/amd64
	docker tag $(ORG)/$(NAME):${VERSION} $(ORG)/$(NAME):latest

push:
	docker push $(ORG)/$(NAME):latest
	docker push $(ORG)/$(NAME):${VERSION}

test: VERSION = latest
test: build
	docker run --rm \
		-u root \
		-v $$(pwd)/home:/home/z/.home \
		--entrypoint zsh \
		-ti $(ORG)/$(NAME)
