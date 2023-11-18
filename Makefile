build-gotty:
	make -C ../webkubectl build

push-gotty:
	docker push krkr/ktty

docker-run-gotty:
	docker run -p 8042:8042 -ti krkr/ktty

kubectl-run-ktty:
	kubectl apply -f ktty.yaml

del:
	kubectl delete po ktty --force
	kubectl delete svc ktty

dev:
	go run main.go

ls:
	@ curl localhost:8080/ls -s | jq -c '.vclusters[]'

lock:
	@ curl localhost:8080/lock
