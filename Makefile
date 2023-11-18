VERSION := $(shell git rev-parse --short HEAD)

# ktty

ktty-release:
	make -C ktty build push

test-ktty-docker:
	docker run -p 8042:8042 -ti krkr/ktty:1571132 \
		--port 8042 --permit-write --credential z:yolo tmux

test-ktty-kube:
	kubectl apply -f ktty.yaml

rm-ktty-kube:
	kubectl delete po ktty --force
	kubectl delete svc ktty

# vktty

build:
	docker buildx build --rm -t krkr/vktty:$(VERSION) . --platform=linux/amd64
	docker tag krkr/vktty:${VERSION} krkr/vktty:latest

push:
	docker push krkr/vktty:latest
	docker push krkr/vktty:$(VERSION)

test: VERSION = latest
test: build
	docker run --rm -p 8080:8080 --entrypoint sh -ti krkr/vktty:$(VERSION)

up:
	sed "s/:latest/:$(VERSION)/" vktty.yaml | \
		kubectl apply -f-

rm:
	kubectl delete -f vktty.yaml

get:
	kubectl -n default get po,svc

logs:
	kubectl -n default logs vktty -f

exec:
	kubectl -n default exec -ti vktty -- bash

ls:
	@curl http://vktty.miaou.space:31325/sudo/ls -s | jq -c '.vclusters[]'

lock:
	@curl http://vktty.miaou.space:31325/lock

# dev

godev:
	go run main.go

devls:
	@ curl localhost:8080/sudo/ls -s | jq -c '.vclusters[]'

devlock:
	@ curl localhost:8080/lock

kgetpo-ktty:
	kubectl get po -l vcluster.loft.sh/namespace=ktty -A -o json | jq '.items[].spec.containers[].args' | grep sudo