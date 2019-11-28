#!/bin/sh
echo $PRIVKEY | /go/bin/tss -http 8080  -port 6668
