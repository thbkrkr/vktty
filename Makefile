ORG 	= krkr
NAME 	= vktty
TAG     = $(shell docker inspect $(ORG)/$(NAME) | jq '.[0].RepoDigests[0]' | sha1sum | cut -c1-10)

all: ktty-build-push build push config up

# ktty

KTTY_TAG = $(shell make -C ktty tag)

ktty-build-push:
	make -C ktty build push
	sed -e "s/KTTY_TAG=.*/KTTY_TAG=$(KTTY_TAG)/" -i .bak .env/.prod.env

# build

build:
	docker buildx build --rm -t $(ORG)/$(NAME):latest . --platform=linux/amd64

push:
	docker tag $(ORG)/$(NAME):latest $(ORG)/$(NAME):$(TAG)
	docker push $(ORG)/$(NAME):latest
	docker push $(ORG)/$(NAME):$(TAG)

test: VERSION = latest
test: build
	docker run --rm -p 8080:8080 --entrypoint sh -ti krkr/vktty:$(VERSION)

# deploy

config:
	kubectl delete secret vktty-envconfig 2> /dev/null || true
	kubectl create secret generic vktty-envconfig --from-env-file=.env/.prod.env

up:
	@sed "s/:latest/:$(TAG)/" vktty.yaml | kubectl apply -f-

del:
	kubectl delete -f vktty.yaml

get:
	kubectl -n default get deploy,po,svc

logs:
	kubectl -n default logs -l app=vktty -f

exec:
	kubectl -n default exec $(shell kubectl -n default get po -l app=vktty -o json | jq -r '.items[0].metadata.name') -ti -- bash

# api

ENV := $(shell cat .env/env)

include .env/.$(ENV).env
export sed 's/=.*//' .env/.$(ENV).env

dev:
	echo dev > .env/env

prod:
	@echo prod > .env/env

sls:
	@curl http://admin:$(VKTTY_BLURB)@$(VKTTY_DOMAIN)/sudo/ls -s | jq -c '.vclusters[]'

ls:
	@curl http://$(VKTTY_DOMAIN)/ls -s | jq -c '.vclusters[]'

lock:
	@curl http://$(VKTTY_DOMAIN)/lock

# dev

godev:
	make dev
	go run main.go

kgetpo-ktty:
	kubectl get po -l vcluster.loft.sh/namespace=ktty -A -o json | jq '.items[].spec.containers[].args' | grep sudo
