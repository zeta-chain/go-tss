#!/bin/bash
echo "nameserver 8.8.8.8">>/etc/resolv.conf
cd /home/user/;go mod init tss
go build /home/user/tss/main.go
/home/user/main
