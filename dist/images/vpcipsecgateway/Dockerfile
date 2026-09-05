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
    strongswan \
    strongswan-swanctl

WORKDIR /kube-ovn
COPY ipsec-gateway.sh /kube-ovn/
RUN chmod +x /kube-ovn/ipsec-gateway.sh
