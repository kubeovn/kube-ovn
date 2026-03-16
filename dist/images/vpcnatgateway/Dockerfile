FROM alpine:edge

RUN set -ex \
    && apk update \
    && apk upgrade \
    && apk add --no-cache \
    bash \
    iproute2 \
    iptables iptables-legacy \
    iputils \
    tcpdump \
    netcat-openbsd \
    conntrack-tools

WORKDIR /kube-ovn
COPY nat-gateway.sh /kube-ovn/
COPY lb-svc.sh /kube-ovn/

# Container startup command. The controller does not override this,
# keeping startup behavior decoupled and managed by the image.
CMD ["sleep", "infinity"]
