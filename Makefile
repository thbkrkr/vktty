ORG 	= krkr
NAME 	= vktty

export TAG = $(shell cat Dockerfile main.go go.* bootstrap/* | sha1sum | cut -c1-10)

all: build config kup

# ktty

ktty-tag:
	sed -e "s/KTTY_TAG=.*/KTTY_TAG=$(shell make -C ../ktty tag)/" -i .bak .env/.prod.env

ktty-build-push:
	make -C ktty build push
	sed -e "s/KTTY_TAG=.*/KTTY_TAG=$(shell make -C ../ktty tag)/" -i .bak .env/.prod.env

# build

build:
	docker buildx build --rm -t $(ORG)/$(NAME):latest -t $(ORG)/$(NAME):$(TAG) \
		. --platform=linux/amd64 --push

push:
	docker tag $(ORG)/$(NAME):latest $(ORG)/$(NAME):$(TAG)
	docker push $(ORG)/$(NAME):latest
	docker push $(ORG)/$(NAME):$(TAG)

test: VERSION = latest
test: build
	docker run --rm -p 8080:8080 --entrypoint sh -ti krkr/vktty:$(VERSION)

# deploy

# config hash is used as deployment label
export CONFIG_HASH = $(shell sha1sum .env/.prod.env | cut -c1-8)

check:
	@bash -c "diff <(make versions_expected | sort) <(make versions_running | sort)" && echo ok

versions_expected:
	@echo KTTY_TAG=$(shell git ls-remote https://github.com/thbkrkr/ktty | head -1 | cut -c1-7)
	@echo VKTTY_TAG=$(TAG)
	@echo CONFIG_HASH=${CONFIG_HASH}

versions_running:
	@ksecret vktty-config | grep KTTY_TAG | sed "s/.*KTTY_TAG:[[:blank:]]*/KTTY_TAG=/"
	@kubectl get deploy -o yaml | grep -E "image:|config:" | \
		sed -e "s/.*config: /CONFIG_HASH=/" \
			-e "s/.*image:.*:/VKTTY_TAG=/"

config:
	kubectl delete secret vktty-config 2> /dev/null || true
	kubectl create secret generic vktty-config --from-env-file=.env/.prod.env

# for testing
manifests:
	envsubst < vktty.yaml

kup:
	envsubst < vktty.yaml | kubectl apply -f-

kdel:
	kubectl delete -f vktty.yaml

kget:
	kubectl -n default get deploy,po,svc

klogs:
	kubectl -n default logs -l app=vktty -f

kexec:
	kubectl -n default exec $(shell kubectl -n default get po -l app=vktty -o json | jq -r '.items[0].metadata.name') -ti -- bash

# api

ENV := $(shell cat .env/env)

include .env/.$(ENV).env
export .env/.$(ENV).env

dev:
	echo dev > .env/env

prod:
	@echo prod > .env/env

sls:
	@curl http://admin:$(VKTTY_BLURB)@$(VKTTY_DOMAIN):$(VKTTY_PORT)/sudo/ls -s | jq -c '.vclusters[]'

ls:
	@curl http://$(VKTTY_DOMAIN):$(VKTTY_PORT)/ls -s | jq -c '.vclusters[]'

get:
	@curl http://$(VKTTY_DOMAIN):$(VKTTY_PORT)/get -s | jq

info:
	curl http://$(VKTTY_DOMAIN):$(VKTTY_PORT)/info -s | jq

ready:
	curl http://$(VKTTY_DOMAIN):$(VKTTY_PORT)/ready -s | jq

metrics:
	@curl http://$(VKTTY_DOMAIN):$(VKTTY_PORT)/metrics -s | grep '^vk_'

# dev

godev:
	echo "$$(cat .env/.dev.env | xargs) go run main.go" | sh 

kgetpo-ktty:
	kubectl get po -l vcluster.loft.sh/namespace=ktty -A -o json | jq '.items[].spec.containers[].args' | grep sudo
