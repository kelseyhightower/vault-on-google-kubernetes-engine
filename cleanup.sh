#!/bin/bash

kubectl delete svc vault vault-0 vault-1
kubectl delete deploy vault-0 vault-1
kubectl delete configmap vault vault-0 vault-1
kubectl delete secrets vault

COMPUTE_REGION=$(gcloud config get-value compute/region)
PROJECT_ID=$(gcloud config get-value project)

gcloud compute addresses delete vault \
  --quiet \
  --region ${COMPUTE_REGION}

gcloud iam service-accounts delete \
  vault-server@${PROJECT_ID}.iam.gserviceaccount.com \
  --quiet

rm *.pem *.csr

gsutil -m rm -r gs://${PROJECT_ID}-vault-storage/*
gsutil rb gs://${PROJECT_ID}-vault-storage

gcloud container clusters delete vault --quiet
