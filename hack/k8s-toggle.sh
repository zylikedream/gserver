#!/bin/bash
# Usage: bash hack/k8s-toggle.sh start|stop
#   start — 启动所有服务
#   stop  — 关闭所有服务（缩到 0）

set -e

ACTION="$1"
case "$ACTION" in
start)
    kubectl scale deployment/gate --replicas=1
    kubectl scale statefulset/role  --replicas=1
    kubectl scale statefulset/chat   --replicas=1
    kubectl scale statefulset/friend --replicas=1
    kubectl scale statefulset/guild  --replicas=1
    echo "All servers started."
    ;;
stop)
    kubectl scale deployment/gate --replicas=0
    kubectl scale statefulset/role  --replicas=0
    kubectl scale statefulset/chat   --replicas=0
    kubectl scale statefulset/friend --replicas=0
    kubectl scale statefulset/guild  --replicas=0
    echo "All servers stopped."
    ;;
*)
    echo "Usage: bash hack/k8s-toggle.sh start|stop"
    exit 1
    ;;
esac
