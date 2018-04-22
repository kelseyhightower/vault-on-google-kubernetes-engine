# Vault on Google Kubernetes Engine

## Tutorial

Gather configuration information:

```
PROJECT_ID=$(gcloud config get-value project)
```

```
COMPUTE_ZONE=$(gcloud config get-value compute/zone)
```

Create GCS bucket:

```
gsutil mb gs://${PROJECT_ID}-vault-storage
```

Create the `vault` service account:

```
gcloud iam service-accounts create vault \
  --display-name "vault service account"
```

```
gcloud iam service-accounts keys create \
  service-account.json \
  --iam-account vault@${PROJECT_ID}.iam.gserviceaccount.com
```

```
gsutil iam ch \
  serviceAccount:vault@${PROJECT_ID}.iam.gserviceaccount.com:objectAdmin \
  gs://${PROJECT_ID}-vault-storage
```

```
gsutil iam ch \
  serviceAccount:vault@${PROJECT_ID}.iam.gserviceaccount.com:legacyBucketReader \
  gs://${PROJECT_ID}-vault-storage
```

### Provision a Kubernetes Cluster

```
gcloud container clusters create vault \
  --enable-autorepair \
  --cluster-version 1.9.6-gke.1 \
  --machine-type n1-standard-2 \
  --service-account vault \
  --num-nodes 3 \
  --zone ${COMPUTE_ZONE}
```
