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
    conntrack-tools

WORKDIR /kube-ovn
# Keep pkg/controller/vpc-endpoint-stitcher.sh in sync (controller embeds it into the ConfigMap).
COPY nat-gateway.sh lb-svc.sh vpc-endpoint-stitcher.sh /kube-ovn/
RUN chmod +x /kube-ovn/nat-gateway.sh /kube-ovn/lb-svc.sh /kube-ovn/vpc-endpoint-stitcher.sh
