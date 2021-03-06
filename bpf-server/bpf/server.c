#include "bpf.h"
#include "bpf_helpers.h"
#include <linux/icmp.h>
#include <linux/if_ether.h>
#include <linux/ip.h>
#include <linux/types.h>
#include <stdint.h>

const volatile __u8 hi_response[] = {0x68, 0x69, 0x0a};
const volatile __u8 hi_rev[] = {0x0a, 0x69, 0x68};
const volatile char hi_char[] = "hi";

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
  __u32 port = __constant_ntohl(skb->remote_port);
	bpf_skb_pull_data(skb, skb->len);
	void *data = (void *)(long)skb->data;
	void *data_end = (void *)(long)skb->data_end;
//	bpf_printk("got data len %d from port %d: %s", data_end - data, port, data);
	uint64_t f1 = 0x0a0d0a0d30203a68;
	uint64_t f2 = 0x74676e656c2d746e;
	uint64_t f3 = 0x65746e6f630a0d4b;
	uint64_t f4 = 0x4f4b4f2030303220;
	uint64_t f5 = 0x312e312f50545448;
	uint64_t size =
		sizeof(f1) + sizeof(f2) + sizeof(f3) + sizeof(f4) + sizeof(f5);

	signed int delta = size - (skb->data_end - skb->data);
//	bpf_printk("adjust by %d", delta);
	int rerr = bpf_skb_adjust_room(skb, delta, 0, 0);
	if (rerr != 0) {
//		bpf_printk("failed to ajust room: %d %d %d", rerr, skb->data_end - skb->data, delta);
		return SK_DROP;
	}

	data = (void *)(long)skb->data;
	data_end = (void *)(long)skb->data_end;
	if (data + size > data_end) {
		return SK_DROP;
	}

	bpf_skb_store_bytes(skb, 32, &f1, sizeof(f1), 0);
	bpf_skb_store_bytes(skb, 24, &f2, sizeof(f2), 0);
	bpf_skb_store_bytes(skb, 16, &f3, sizeof(f3), 0);
	bpf_skb_store_bytes(skb, 8, &f4, sizeof(f4), 0);
	bpf_skb_store_bytes(skb, 0, &f5, sizeof(f5), 0);
//	bpf_printk("got updated data len %d: %s", skb->len, data);

	int err = bpf_sk_redirect_map(skb, &sock_map, port, 0);
	return err;
}

char _license[4] SEC("license") = "GPL";