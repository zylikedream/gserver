#!/bin/bash
# Usage: bash hack/k8s-toggle.sh start|stop
#   start — 启动所有服务
#   stop  — 关闭所有服务（缩到 0）

set -e

ACTION="$1"
case "$ACTION" in
start)
    kubectl scale deployment/gate --replicas=2
    kubectl scale gss/role  --replicas=2
    kubectl scale gss/chat   --replicas=2
    kubectl scale gss/friend --replicas=2
    kubectl scale gss/guild  --replicas=2
    echo "All servers started."
    ;;
stop)
    kubectl scale gss/guild  --replicas=0
    kubectl scale gss/chat   --replicas=0
    kubectl scale gss/friend --replicas=0
    kubectl scale gss/role  --replicas=0
    kubectl scale deployment/gate --replicas=0
    echo "All servers stopped."
    ;;
*)
    echo "Usage: bash hack/k8s-toggle.sh start|stop"
    exit 1
    ;;
esac
