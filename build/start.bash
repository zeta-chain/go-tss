#!/bin/sh
echo $PRIVKEY | /go/bin/tss -tss-port :8080  -p2p-port 6668 -loglevel debug
