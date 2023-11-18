FROM krkr/gotty

RUN apk --update --no-cache \
    bash zsh tmux \
    make git wget curl tree unzip

ENV KUBECTL_VERSION=1.28.4
RUN curl -fsSLO https://dl.k8s.io/v${KUBECTL_VERSION}/bin/linux/amd64/kubectl && \
    mv kubectl /usr/local/bin/kubectl && chmod +x /usr/local/bin/kubectl

ENV HELM_VERSION=3.13.1
RUN curl -fsSL -o get_helm.sh https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 && \
    chmod 700 get_helm.sh && ./get_helm.sh -v v${HELM_VERSION} --no-sudo && rm get_helm.sh
