#include <linux/module.h>
#include <linux/kernel.h>
#include <linux/skbuff.h>
#include <linux/ip.h>
#include <linux/netfilter.h>
#include <linux/netfilter_ipv4.h>
#include <linux/udp.h>
#include <linux/tcp.h>
#include <linux/string.h>

static struct nf_hook_ops local_in;
static struct nf_hook_ops pre_routing;
static struct nf_hook_ops post_routing;
static struct nf_hook_ops local_out;

unsigned int hook_func(unsigned int hooknum,
                    struct sk_buff *skb,
                    const struct net_device *in,
                    const struct net_device *out,
                    int (*okfn)(struct sk_buff *))
{
    struct iphdr *ip1 = NULL;
    struct udphdr *udp_header = NULL;
    struct tcphdr *tcp_header = NULL;

    // For container network traffic, DO NOT traverse netfilter
    if (NULL != in && NULL != in->ifalias && in->ifalias[13] == 'c' ) { return NF_STOP; }
    if (NULL != out && NULL != out->ifalias && out->ifalias[13] == 'c' ) { return NF_STOP; }

    if (!skb){
        return NF_ACCEPT;
    }

    // for Geneve Tunnel traffic, DO NOT traverse netfilter
    ip1 = ip_hdr(skb);
    if (NULL != ip1) {
        if (IPPROTO_UDP == ip1->protocol) {
            udp_header = (struct udphdr *)skb_transport_header(skb);
            if (ntohs(udp_header->dest) == 6081 || ntohs(udp_header->source) == 6081) {
                return NF_STOP;
            }
        }
    }

    // for STT Tunnel traffic, DO NOT traverse netfilter
    if (hooknum != NF_INET_LOCAL_IN && NULL != ip1) {
        if (IPPROTO_TCP == ip1->protocol) {
            tcp_header = (struct tcphdr *)skb_transport_header(skb);
            if (ntohs(tcp_header->dest) == 7471) {
                return NF_STOP;
            }
        }
    }

    return NF_ACCEPT;
}

int init_module()
{
    local_out.hook     = hook_func;
    local_out.hooknum  = NF_INET_LOCAL_OUT;
    local_out.pf       = PF_INET;
    local_out.priority = NF_IP_PRI_FIRST;
    nf_register_hook(&local_out);
    printk("init_module,kube_ovn_fastpath_local_out\n");

    post_routing.hook     = hook_func;
    post_routing.hooknum  = NF_INET_POST_ROUTING;
    post_routing.pf       = PF_INET;
    post_routing.priority = NF_IP_PRI_FIRST;
    nf_register_hook(&post_routing);
    printk("init_module,kube_ovn_fastpath_post_routing\n");

    pre_routing.hook     = hook_func;
    pre_routing.hooknum  = NF_INET_PRE_ROUTING;
    pre_routing.pf       = PF_INET;
    pre_routing.priority = NF_IP_PRI_FIRST;
    nf_register_hook(&pre_routing);
    printk("init_module,kube_ovn_fastpath_pre_routing\n");

    local_in.hook     = hook_func;
    local_in.hooknum  = NF_INET_LOCAL_IN;
    local_in.pf       = PF_INET;
    local_in.priority = NF_IP_PRI_FIRST;
    nf_register_hook(&local_in);
    printk("init_module,kube_ovn_fastpath_local_in\n");

    return 0;
}

/* Cleanup routine */
void cleanup_module()
{
    nf_unregister_hook(&local_in);
    nf_unregister_hook(&pre_routing);
    nf_unregister_hook(&post_routing);
    nf_unregister_hook(&local_out);
    printk("cleanup_module,kube_ovn_fastpath\n");
}

MODULE_LICENSE("GPL");
