FROM alpine:edge

RUN set -ex \
    && apk update \
    && apk upgrade \
    && apk add --no-cache \
    bash \
    iproute2 \
    iptables iptables-legacy \
    nftables \
    iputils \
    tcpdump \
    conntrack-tools \
    wireguard-tools

WORKDIR /kube-ovn
COPY nat-gateway.sh lb-svc.sh wireguard.sh /kube-ovn/
RUN chmod +x /kube-ovn/nat-gateway.sh /kube-ovn/lb-svc.sh /kube-ovn/wireguard.sh
