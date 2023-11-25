ORG 	= krkr
NAME 	= ktty

TAG     = $(shell echo "$(shell find home -type f | xargs cat | sha1sum) $(shell cat Dockerfile | sha1sum)" | sha1sum | cut -c1-10)

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
	docker run -p 8042:8042 -ti -e GOTTY_CREDENTIAL=z:yolo krkr/ktty:latest \
		--port 8042 --permit-write tmux

test-up:
	kubectl apply -f ktty.yaml

test-del:
	kubectl delete -f ktty.yaml