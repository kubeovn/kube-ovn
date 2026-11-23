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
    libcap-utils \
    # capsh carries the container's permitted capabilities through a non-root
    # shell with no_new_privs. Resolve symlinks to cover all xtables applets.
    && for tool in capsh iptables iptables-legacy; do \
        setcap cap_net_admin,cap_net_raw=ep "$(readlink -f "$(command -v "$tool")")" || exit 1; \
    done \
    && setcap cap_net_admin=ep "$(readlink -f "$(command -v ip)")" \
    && setcap cap_net_raw=ep "$(readlink -f "$(command -v arping)")" \
    # Keep the standard lock usable by both the root NAT gateway and lb-svc.
    && touch /run/xtables.lock \
    && chgrp 65532 /run/xtables.lock \
    && chmod 0660 /run/xtables.lock

WORKDIR /kube-ovn
COPY nat-gateway.sh /kube-ovn/
COPY lb-svc.sh /kube-ovn/
