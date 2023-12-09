FROM golang:1.21-alpine as go-builder

WORKDIR /work

COPY go.mod go.sum ./
RUN --mount=type=cache,mode=0755,target=/go/pkg/mod go mod download
COPY main.go /work/
RUN --mount=type=cache,mode=0755,target=/go/pkg/mod CGO_ENABLED=0 GO111MODULE=on go build

FROM alpine:3.18.4

RUN apk add --update --no-cache curl bash envsubst jq
RUN curl -L -o vcluster "https://github.com/loft-sh/vcluster/releases/download/v0.17.1/vcluster-linux-amd64" && \
    chmod +x vcluster && mv vcluster /usr/bin
RUN curl -fsSLO https://dl.k8s.io/v1.28.4/bin/linux/amd64/kubectl && \
    mv kubectl /usr/local/bin/kubectl && chmod +x /usr/local/bin/kubectl

COPY --from=go-builder /work/vktty /usr/bin/
COPY bootstrap /usr/local/bin/bootstrap

WORKDIR /usr/local/bin/
ENTRYPOINT ["vktty"]
