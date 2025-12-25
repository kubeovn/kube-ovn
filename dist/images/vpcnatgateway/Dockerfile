FROM alpine:edge

ARG CN=false

RUN set -ex \
    && if [ "$CN" = "true" ]; then \
    sed -i 's#https://dl-cdn.alpinelinux.org/alpine/edge/#http://mirrors.aliyun.com/alpine/edge/#g' /etc/apk/repositories; \
    fi \
    && apk update \
    && apk upgrade \
    && apk add --no-cache \
    bash \
    iproute2 \
    iptables iptables-legacy \
    iputils \
    tcpdump \
    conntrack-tools

WORKDIR /kube-ovn
COPY nat-gateway.sh /kube-ovn/
COPY lb-svc.sh /kube-ovn/
