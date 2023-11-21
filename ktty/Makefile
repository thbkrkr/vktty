ORG 	= krkr
NAME 	= ktty
TAG     := $(shell docker inspect $(ORG)/$(NAME) | jq '.[0].RepoDigests[0]' | sha1sum | cut -c1-10)

all: build push

tag:
	@echo $(TAG)

build:
	docker buildx build --rm -t $(ORG)/$(NAME):latest . --platform=linux/amd64

push:
	docker tag $(ORG)/$(NAME):latest $(ORG)/$(NAME):$(TAG)
	docker push $(ORG)/$(NAME):latest
	docker push $(ORG)/$(NAME):$(TAG)

test: build
	docker run --rm \
		-u root \
		-v $$(pwd)/home:/home/z/.home \
		--entrypoint zsh \
		-ti $(ORG)/$(NAME):latest

test-run:
	docker run -p 8042:8042 -ti krkr/ktty:latest \
		--port 8042 --permit-write --credential z:yolo tmux

test-up:
	kubectl apply -f ktty.yaml

test-del:
	kubectl delete -f ktty.yaml