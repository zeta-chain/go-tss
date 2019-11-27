#!/bin/sh
while ! nc -z 192.168.10.1 8080; do
  echo sleeping
  sleep 1
done

/go/bin/tss -http 8080 -peer /ip4/192.168.10.1/tcp/6668/ipfs/$(curl http://192.168.10.1:8080/p2pid) -port 6668
