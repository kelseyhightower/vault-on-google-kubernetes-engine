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
COMPUTE_REGION=$(gcloud config get-value compute/region)
```

```
GCS_BUCKET_NAME="${PROJECT_ID}-vault-storage"
```

### Provision IP Address

```
gcloud compute addresses create vault \
  --region ${COMPUTE_REGION}
```

```
gcloud compute addresses create vault-0 \
  --region ${COMPUTE_REGION}
```

```
gcloud compute addresses create vault-1 \
  --region ${COMPUTE_REGION}
```

```
VAULT_LOAD_BALANCER_IP=$(gcloud compute addresses describe vault \
  --region ${COMPUTE_REGION} --format='value(address)')
```

```
VAULT_0_LOAD_BALANCER_IP=$(gcloud compute addresses describe vault-0 \
  --region ${COMPUTE_REGION} --format='value(address)')
```

```
VAULT_1_LOAD_BALANCER_IP=$(gcloud compute addresses describe vault-1 \
  --region ${COMPUTE_REGION} --format='value(address)')
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
  -hostname="vault,vault.default.svc.cluster.local,0.vault.default.svc.cluster.local,1.vault.default.svc.cluster.local,localhost,127.0.0.1,${VAULT_LOAD_BALANCER_IP},${VAULT_0_LOAD_BALANCER_IP},${VAULT_1_LOAD_BALANCER_IP}" \
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
  --from-literal api-addr=https://${VAULT_0_LOAD_BALANCER_IP}:8200
```

```
kubectl create configmap vault-1 \
  --from-literal api-addr=https://${VAULT_1_LOAD_BALANCER_IP}:8200
```

### Deploy Vault

```
kubectl apply -f vault.yaml
```

```
mkdir services
```

```
cat > services/vault.yaml <<EOF
apiVersion: v1
kind: Service
metadata:
  name: vault
spec:
  type: LoadBalancer
  loadBalancerIP: ${VAULT_LOAD_BALANCER_IP}
  ports:
    - name: http
      port: 8200
    - name: server
      port: 8201
  selector:
    app: vault
EOF
```

```
cat > services/vault-0.yaml <<EOF
apiVersion: v1
kind: Service
metadata:
  name: vault-0
spec:
  type: LoadBalancer
  loadBalancerIP: ${VAULT_0_LOAD_BALANCER_IP}
  ports:
    - name: http
      port: 8200
    - name: server
      port: 8201
  selector:
    app: vault
    instance: "0"
EOF
```

```
cat > services/vault-1.yaml <<EOF
apiVersion: v1
kind: Service
metadata:
  name: vault-1
spec:
  type: LoadBalancer
  loadBalancerIP: ${VAULT_1_LOAD_BALANCER_IP}
  ports:
    - name: http
      port: 8200
    - name: server
      port: 8201
  selector:
    app: vault
    instance: "1"
EOF
```

```
kubectl apply -f services
```

Initialize Vault:

```
source vault.env
```

```
vault operator init
```
