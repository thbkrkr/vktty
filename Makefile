ORG 	= krkr
NAME 	= vktty
TAG     := $(shell docker inspect $(ORG)/$(NAME) | jq '.[0].RepoDigests[0]' | sha1sum | cut -c1-10)

all: ktty-build-push build push up

# ktty

KTTY_TAG := $(shell make -C ktty tag)

ktty-build-push:
	make -C ktty build push

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

SECRET := $(shell cat .secret)

up:
	@sed -e "s/value: latest/value: $(KTTY_TAG)/" -e "s/:latest/:$(TAG)/" -e "s/secret/$(SECRET)/" vktty.yaml | \
		kubectl apply -f-

del:
	kubectl delete -f vktty.yaml

get:
	kubectl -n default get po,svc

logs:
	kubectl -n default logs vktty -f

exec:
	kubectl -n default exec -ti vktty -- bash

# api

API    := vktty.miaou.space:31319

sls:
	@curl http://admin:$(SECRET)@$(API)/sudo/ls -s | jq -c '.vclusters[]'

ls:
	@curl http://$(API)/ls -s | jq -c '.vclusters[]'

lock:
	@curl http://$(API)/lock

# dev

godev:
	SECRET=dev KTTY_TAG=$(VERSION) go run main.go

devls:
	@ curl admin:dev@localhost:8080/sudo/ls -s | jq -c '.vclusters[]'

devlock:
	@ curl localhost:8080/lock

kgetpo-ktty:
	kubectl get po -l vcluster.loft.sh/namespace=ktty -A -o json | jq '.items[].spec.containers[].args' | grep sudo
