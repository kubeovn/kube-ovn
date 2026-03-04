#include <linux/module.h>
#include <linux/kernel.h>
#include <linux/skbuff.h>
#include <linux/ip.h>
#include <linux/netfilter.h>
#include <linux/netfilter_ipv4.h>
#include <linux/udp.h>
#include <linux/tcp.h>
#include <linux/string.h>
#include <linux/inetdevice.h>

unsigned int hook_func(void *priv,
                    struct sk_buff *skb, const struct nf_hook_state *state)
{
    struct iphdr *ip_header = NULL;
    struct udphdr *udp_header = NULL;
    struct tcphdr *tcp_header = NULL;

    if (!skb){
        return NF_ACCEPT;
    }

    if (state->net == &init_net) {
        // for Geneve Tunnel traffic, DO NOT traverse netfilter
        ip_header = ip_hdr(skb);
        if (NULL != ip_header) {
            if (IPPROTO_UDP == ip_header->protocol) {
                udp_header = (struct udphdr *)skb_transport_header(skb);
                if (ntohs(udp_header->dest) == 6081 || ntohs(udp_header->source) == 6081) {
                    state->okfn(state->net, state->sk, skb);
                    return NF_STOLEN;
                }
            } else {
                // for STT Tunnel traffic, DO NOT traverse netfilter
                if (IPPROTO_TCP == ip_header->protocol && state->hook != NF_INET_LOCAL_IN) {
                    tcp_header = (struct tcphdr *)skb_transport_header(skb);
                    if (ntohs(tcp_header->dest) == 7471) {
                        state->okfn(state->net, state->sk, skb);
                        return NF_STOLEN;
                    }
                }
            }
        }
        return NF_ACCEPT;
    }

    if (state->net != &init_net) {
        /*
         * Skip fastpath for namespaces with IP forwarding enabled.
         *
         * Fastpath calls okfn() directly and returns NF_STOLEN, which
         * bypasses ALL subsequent netfilter hooks in the chain. This
         * skips the following critical subsystems:
         *
         *   - conntrack (nf_conntrack_in, priority -200):
         *     Connection tracking is disabled. Without conntrack,
         *     stateful NAT cannot function because the kernel cannot
         *     associate reply packets with original connections.
         *
         *   - NAT (nft_do_chain / iptable_nat, priority -100):
         *     DNAT/SNAT rules are never evaluated. For DNAT, inbound
         *     destination rewriting fails. For return traffic, conntrack's
         *     automatic reverse NAT also fails since no conntrack entry
         *     exists.
         *
         *   - nftables/iptables filter chains:
         *     All firewall rules (INPUT/FORWARD/OUTPUT filter tables)
         *     are bypassed, disabling any security policies.
         *
         *   - mangle table:
         *     TOS/DSCP marking, TTL modification, and policy routing
         *     via fwmark are all skipped.
         *
         * For normal pods (forwarding=0), traffic is endpoint-only and
         * managed by OVN at the virtual switch layer. Skipping netfilter
         * is safe and improves performance.
         *
         * For gateway pods like vpc-nat-gw (forwarding=1), the pod acts
         * as a router performing DNAT/SNAT between external networks
         * (macvlan) and the overlay (veth). Both inbound and return
         * traffic must traverse the full netfilter stack, so fastpath
         * must be disabled for the gw pod namespace.
         */
        if (IPV4_DEVCONF_ALL(state->net, FORWARDING))
            return NF_ACCEPT;

        state->okfn(state->net, state->sk, skb);
        return NF_STOLEN;
    }

    return NF_ACCEPT;
}

static const struct nf_hook_ops fast_path_nf_ops[] = {
	{
		.hook		= hook_func,
		.pf		    = PF_INET,
		.hooknum	= NF_INET_LOCAL_IN,
		.priority	= NF_IP_PRI_FIRST,
	},
		{
        .hook		= hook_func,
        .pf		    = PF_INET,
        .hooknum	= NF_INET_LOCAL_OUT,
        .priority	= NF_IP_PRI_FIRST,
    },
    {
        .hook		= hook_func,
        .pf		    = PF_INET,
        .hooknum	= NF_INET_POST_ROUTING,
        .priority	= NF_IP_PRI_FIRST,
    },
    {
        .hook		= hook_func,
        .pf		    = PF_INET,
        .hooknum	= NF_INET_PRE_ROUTING,
        .priority	= NF_IP_PRI_FIRST,
    },
};

static int __net_init __fast_path_init(struct net *net)
{
    return nf_register_net_hooks(net, fast_path_nf_ops, 4);
}

static void __net_exit __fast_path_cleanup(struct net *net)
{
	nf_unregister_net_hooks(net, fast_path_nf_ops, 4);
}

static struct pernet_operations fast_path_ops = {
	.init = __fast_path_init,
	.exit = __fast_path_cleanup,
};


static int __init fast_path_init(void)
{
    printk("init_module,kube_ovn_fast_path\n");
    return register_pernet_subsys(&fast_path_ops);
}

/* Cleanup routine */
static void __exit fast_path_cleanup(void)
{
    printk("cleanup_module,kube_ovn_fast_path\n");
	unregister_pernet_subsys(&fast_path_ops);
}

module_init(fast_path_init);
module_exit(fast_path_cleanup);
MODULE_LICENSE("GPL");
