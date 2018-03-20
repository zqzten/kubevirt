#!/bin/bash
#
# This file is part of the KubeVirt project
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#
# Copyright 2017 Red Hat, Inc.
#

# CI considerations: $TARGET is used by the jenkins vagrant build, to distinguish what to test
# Currently considered $TARGET values:
#     vagrant-dev: Runs all functional tests on a development vagrant setup
#     vagrant-release: Runs all possible functional tests on a release deployment in vagrant
#     TODO: vagrant-tagged-release: Runs all possible functional tests on a release deployment in vagrant on a tagged release

set -ex

export WORKSPACE="${WORKSPACE:-$PWD}"
export PROVIDER="k8s-1.9.3"
export VAGRANT_NUM_NODES=1

kubectl() { cluster/kubectl.sh "$@"; }

export NAMESPACE="${NAMESPACE:-kube-system}"

# Make sure that the VM is properly shut down on exit
trap '{ make cluster-down; }' EXIT

make cluster-down
make cluster-up

# Wait for nodes to become ready
while [ -n "$(kubectl get nodes --no-headers | grep -v Ready)" ]; do
   echo "Waiting for all nodes to become ready ..."
   kubectl get nodes --no-headers | >&2 grep -v Ready || true
   sleep 10
done
echo "Nodes are ready:"
kubectl get nodes

make cluster-sync

# Wait until kubevirt pods are running
while [ -n "$(kubectl get pods -n ${NAMESPACE} --no-headers | grep -v Running)" ]; do
    echo "Waiting for kubevirt pods to enter the Running state ..."
    kubectl get pods -n ${NAMESPACE} --no-headers | >&2 grep -v Running || true
    sleep 10
done

# Make sure all containers except virt-controller are ready
while [ -n "$(kubectl get pods -n ${NAMESPACE} -o'custom-columns=status:status.containerStatuses[*].ready,metadata:metadata.name' --no-headers | awk '!/virt-controller/ && /false/')" ]; do
    echo "Waiting for KubeVirt containers to become ready ..."
    kubectl get pods -n ${NAMESPACE} -o'custom-columns=status:status.containerStatuses[*].ready,metadata:metadata.name' --no-headers | awk '!/virt-controller/ && /false/' || true
    sleep 10
done

# Make sure that at least one virt-controller container is ready
while [ "$(kubectl get pods -n ${NAMESPACE} -o'custom-columns=status:status.containerStatuses[*].ready,metadata:metadata.name' --no-headers | awk '/virt-controller/ && /true/' | wc -l)" -lt "1" ]; do
    echo "Waiting for KubeVirt virt-controller container to become ready ..."
    kubectl get pods -n ${NAMESPACE} -o'custom-columns=status:status.containerStatuses[*].ready,metadata:metadata.name' --no-headers | awk '/virt-controller/ && /true/' | wc -l
    sleep 10
done

kubectl get pods -n ${NAMESPACE}
kubectl version

# Run functional tests
FUNC_TEST_ARGS="--ginkgo.noColor" make functest
