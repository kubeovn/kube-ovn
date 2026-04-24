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
    conntrack-tools

WORKDIR /kube-ovn
COPY nat-gateway.sh /kube-ovn/
COPY lb-svc.sh /kube-ovn/

# Default startup command. The controller currently also sets the same command explicitly
# for backward compatibility with older images that did not define CMD.
CMD ["sleep", "infinity"]
