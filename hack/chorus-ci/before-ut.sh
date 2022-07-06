#!/bin/bash

yum update -y
yum install -y libvirt-devel

mkdir -p /root/kubevirt-kernel-headers
cd /root/kubevirt-kernel-headers

wget https://ack-a-utils.oss-cn-beijing.aliyuncs.com/kernel-headers/kernel-headers-4.18.0-348.2.1.el8_5.x86_64.rpm
rpm2cpio kernel-headers-4.18.0-348.2.1.el8_5.x86_64.rpm | cpio -idmv

export GOPROXY=https://mirrors.aliyun.com/goproxy/,direct
export GOPRIVATE=*gitlab.alibaba-inc.com
export GOSUMDB=off
export CGO_CFLAGS="-I/root/kubevirt-kernel-headers/usr/include"

cd -