# Vault on Google Kubernetes Engine

## Tutorial

Gather configuration information:

```
PROJECT_ID=$(gcloud config get-value project)
```

```
COMPUTE_ZONE=$(gcloud config get-value compute/zone)
```

```
GCS_BUCKET_NAME="${PROJECT_ID}-vault-storage"
```

### Provision IP Address

```
gcloud compute addresses create vault --global
```

```
gcloud compute addresses create vault-0 --global
```

```
gcloud compute addresses create vault-1 --global
```

```
VAULT_ADDRESS=(gcloud compute addresses describe vault \
  --global --format='value(address)')
```

```
VAULT_0_ADDRESS=(gcloud compute addresses describe vault-0 \
  --global --format='value(address)')
```

```
VAULT_1_ADDRESS=(gcloud compute addresses describe vault-1 \
  --global --format='value(address)')
```

### Create GCS bucket:

```
gsutil mb gs://${GCS_BUCKET_NAME}
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
  gs://${GCS_BUCKET_NAME}
```

```
gsutil iam ch \
  serviceAccount:vault-server@${PROJECT_ID}.iam.gserviceaccount.com:legacyBucketReader \
  gs://${GCS_BUCKET_NAME}
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

```
cfssl gencert \
  -ca=ca.pem \
  -ca-key=ca-key.pem \
  -config=ca-config.json \
  -hostname="vault,vault.default.svc.cluster.local,0.vault.default.svc.cluster.local,1.vault.default.svc.cluster.local,localhost,127.0.0.1,${VAULT_ADDRESS},${VAULT_0_ADDRESS},${VAULT_1_ADDRESS}" \
  -profile=default \
  vault-csr.json | cfssljson -bare vault
```

Create Kubernetes secret:

```
cat vault.pem ca.pem > vault-combined.pem
```

```
kubectl create secret generic vault \
  --from-file=ca.pem \
  --from-file=vault.pem=vault-combined.pem \
  --from-file=vault-key.pem
```

Create `vault` configmap:

```
cat > vault.hcl <<EOF
disable_mlock = true

listener "tcp" {
  address = "0.0.0.0:8200"
  tls_cert_file = "/etc/vault/tls/vault.pem"
  tls_client_ca_file = "/etc/vault/tls/ca.pem"
  tls_key_file = "/etc/vault/tls/vault-key.pem"
  tls_min_version = "tls12"
  tls_require_and_verify_client_cert = "true"
}

storage "gcs" {
  bucket = "${GCS_BUCKET_NAME}"
  ha_enabled = "true"
}

ui = true
EOF
```

```
kubectl create configmap vault --from-file vault.hcl
```

```
kubectl create configmap vault-0 \
  --from-literal api-addr=https://${VAULT_0_ADDRESS}:8200
```

```
kubectl create configmap vault-1 \
  --from-literal api-addr=https://${VAULT_1_ADDRESS}:8200
```

### Deploy Vault

```
kubectl apply -f vault.yaml
```

Initialize Vault:

```
source vault.env
```

```
vault operator init
```
