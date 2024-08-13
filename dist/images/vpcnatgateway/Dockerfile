FROM alpine:edge

RUN set -ex \
    && apk update \
    && apk upgrade \
    && apk add --no-cache \
    bash \
    iproute2 \
    iptables \
    iputils \
    tcpdump \
    conntrack-tools

WORKDIR /kube-ovn
COPY *.sh /kube-ovn/
RUN /kube-ovn/iptables-wrapper-installer.sh --no-sanity-check
