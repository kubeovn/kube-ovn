FROM alpine:edge

RUN set -ex \
    && apk update \
    && apk upgrade \
    && apk add --no-cache \
    bash \
    iproute2 \
    iptables \
    iputils \
    tcpdump

WORKDIR /kube-ovn
COPY nat-gateway.sh /kube-ovn/
