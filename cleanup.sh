#!/bin/bash

kubectl delete deploy vault-0 vault-1
kubectl delete svc vault vault-0 vault-1
kubectl delete configmap vault vault-0 vault-1
kubectl delete secrets vault
