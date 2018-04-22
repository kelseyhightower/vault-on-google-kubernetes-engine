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
gcloud iam service-accounts create vault-server \
  --display-name "vault service account"
```

```
gcloud iam service-accounts keys create \
  service-account.json \
  --iam-account vault-server@${PROJECT_ID}.iam.gserviceaccount.com
```

```
gsutil iam ch \
  serviceAccount:vault-server@${PROJECT_ID}.iam.gserviceaccount.com:objectAdmin \
  gs://${PROJECT_ID}-vault-storage
```

```
gsutil iam ch \
  serviceAccount:vault-server@${PROJECT_ID}.iam.gserviceaccount.com:legacyBucketReader \
  gs://${PROJECT_ID}-vault-storage
```

### Provision a Kubernetes Cluster

```
gcloud container clusters create vault \
  --enable-autorepair \
  --cluster-version 1.9.6-gke.1 \
  --machine-type n1-standard-2 \
  --service-account vault-server@${PROJECT_ID}.iam.gserviceaccount.com \
  --num-nodes 3 \
  --zone ${COMPUTE_ZONE}
```

### Generate TLS Certificates

Create TLS certificates:

```
cfssl gencert -initca ca-csr.json | cfssljson -bare ca
```

VAULT_HOSTNAME="vault.hightowerlabs.com"

```
cfssl gencert \
  -ca=ca.pem \
  -ca-key=ca-key.pem \
  -config=ca-config.json \
  -hostname="vault,vault.default.svc.cluster.local,localhost,127.0.0.1,${VAULT_HOSTNAME}" \
  -profile=default \
  vault-csr.json | cfssljson -bare vault
```

Create Kubernetes secret:

```
cat ca.pem vault.pem > vault-combined.pem
```


```
kubectl create secret generic vault \
  --from-file=ca.pem \
  --from-file=vault.pem=vault-combined.pem \
  --from-file=vault-key.pem
```
