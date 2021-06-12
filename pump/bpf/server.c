#include "bpf.h"
#include "bpf_helpers.h"
#include <linux/icmp.h>
#include <linux/if_ether.h>
#include <linux/ip.h>
#include <linux/types.h>
#include <stdint.h>

struct bpf_map_def SEC("maps") sock_map = {
	.type = BPF_MAP_TYPE_SOCKMAP,
	.key_size = sizeof(int),
	.value_size = sizeof(int),
	.max_entries = 65535,
};

#define MIN(a, b) ((a) < (b) ? (a) : (b))
#define MAX(a, b) ((a) > (b) ? (a) : (b))

#define ARRAY_SIZE(x) (sizeof(x) / sizeof((x)[0]))

SEC("sk_skb/stream_parser")
int _prog_parser(struct __sk_buff *skb) { return skb->len; }

SEC("sk_skb/stream_verdict")
int _prog_verdict(struct __sk_buff *skb)
{
  return SK_DROP;
  __u32 port = __constant_ntohl(skb->remote_port);
	bpf_skb_pull_data(skb, skb->len);
	void *data = (void *)(long)skb->data;
	void *data_end = (void *)(long)skb->data_end;
	bpf_printk("got stream %s", data);
	return SK_PASS;
}

char _license[4] SEC("license") = "GPL";