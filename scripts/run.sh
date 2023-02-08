#!/usr/bin/env bash

# deploy karpenter
helm upgrade --create-namespace --install karpenter-k3d ~/git/karpenter/charts/karpenter --namespace default --set replicas=0

# remove useless webhooks
kubectl delete mutatingwebhookconfigurations defaulting.webhook.karpenter.k8s.aws defaulting.webhook.karpenter.sh
kubectl delete validatingwebhookconfigurations validation.webhook.karpenter.k8s.aws validation.webhook.karpenter.sh validation.webhook.config.karpenter.sh