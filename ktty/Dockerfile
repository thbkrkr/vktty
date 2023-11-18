FROM krkr/gotty

RUN apk --update --no-cache add \
    bash zsh zsh-vcs tmux \
    openssl make git \
    wget curl  \
    tree unzip \
    iftop htop 

ENV KUBECTL_VERSION=1.28.4
RUN curl -fsSLO https://dl.k8s.io/v${KUBECTL_VERSION}/bin/linux/amd64/kubectl && \
    mv kubectl /usr/local/bin/kubectl && chmod +x /usr/local/bin/kubectl

ENV HELM_VERSION=3.13.2
RUN curl -fsSL -o get_helm.sh https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 && \
    chmod 700 get_helm.sh && ./get_helm.sh -v v${HELM_VERSION} --no-sudo && rm get_helm.sh

RUN adduser -g '' -D  z z

COPY home /home/z/.home
RUN HOME=/home/z /home/z/.home/install.sh && \
    sed -i "s|z:x:1000:1000::/home/z:/bin/ash|z:x:1000:1000::/home/z:/bin/zsh|" /etc/passwd

USER z
WORKDIR /home/z

CMD ["zsh"]