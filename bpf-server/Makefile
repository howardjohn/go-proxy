all:
	clang -Wall -Wextra -O2 -MD -MF server.d -target bpf -I /usr/include/x86_64-linux-gnu/ -c server.c
	clang -target bpf -Wall -O2 -emit-llvm -g -Iinclude -c server.c -o - | \
    	llc -march=bpf -mcpu=probe -filetype=obj -o server.o