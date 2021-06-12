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

struct bpf_map_def SEC("maps") counter = {
	.type = BPF_MAP_TYPE_HASH,
	.key_size = sizeof(__u64),
	.value_size = sizeof(__u64),
	.max_entries = 32,
};

#define MIN(a, b) ((a) < (b) ? (a) : (b))
#define MAX(a, b) ((a) > (b) ? (a) : (b))

#define ARRAY_SIZE(x) (sizeof(x) / sizeof((x)[0]))

SEC("sk_skb/stream_parser")
int _prog_parser(struct __sk_buff *skb) { return skb->len; }

SEC("sk_skb/stream_verdict")
int _prog_verdict(struct __sk_buff *skb)
{
  __u64 zero = 0, *val;
  __u64 key = 0;
	val = bpf_map_lookup_elem(&counter, &key);
	if (!val) {
		bpf_map_update_elem(&counter, &key, &zero, BPF_NOEXIST);
		val = bpf_map_lookup_elem(&counter, &key);
		if (!val)
			return SK_DROP;
	}
	(*val) += 1;
	return SK_DROP;
}

char _license[4] SEC("license") = "GPL";