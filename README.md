# Vault on Google Kubernetes Engine

This tutorial walks you through provisioning a two node [HashiCorp Vault](https://www.vaultproject.io/intro/index.html) cluster on [Google Kubernetes Engine](https://cloud.google.com/kubernetes-engine).

## Cluster Features

* High Availability - The Vault cluster will be provisioned in [multi-server mode](https://www.vaultproject.io/docs/concepts/ha.html) for high availability.
* Google Cloud Storage Storage Backend - Vault's data is persisted in [Google Cloud Storage](https://cloud.google.com/storage).
* Production Hardening - Vault is configured and deployed based on the guidance found in the [production hardening](https://www.vaultproject.io/guides/operations/production.html) guide.
* Auto Initialization and Unsealing - Vault is automatically initialized and unsealed at runtime. Keys are encrypted using [Cloud KMS](https://cloud.google.com/kms) and stored in on [Google Cloud Storage](https://cloud.google.com/storage).

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

```
KMS_KEY_ID="projects/${PROJECT_ID}/locations/global/keyRings/vault/cryptoKeys/vault-init"
```

### Create KMS Keyring and Crypto Key

```
gcloud kms keyrings create vault \
  --location global
```

```
gcloud kms keys create vault-init \
  --location global \
  --keyring vault \
  --purpose encryption
```

### Create GCS bucket:

```
gsutil mb gs://${GCS_BUCKET_NAME}
```

### Create the Vault IAM Service Account

Create the `vault` service account:

```
gcloud iam service-accounts create vault-server \
  --display-name "vault service account"
```

Grant access to the vault storage bucket:

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

Grant access to the vault init keys:

```
gcloud kms keys add-iam-policy-binding \
  vault-init \
  --location global \
  --keyring vault \
  --member serviceAccount:vault-server@${PROJECT_ID}.iam.gserviceaccount.com \
  --role roles/cloudkms.cryptoKeyEncrypterDecrypter
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

### Provision IP Address

```
gcloud compute addresses create vault \
  --region ${COMPUTE_REGION}
```

```
VAULT_LOAD_BALANCER_IP=$(gcloud compute addresses describe vault \
  --region ${COMPUTE_REGION} --format='value(address)')
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
  -hostname="vault,vault.default.svc.cluster.local,localhost,127.0.0.1,${VAULT_LOAD_BALANCER_IP}" \
  -profile=default \
  vault-csr.json | cfssljson -bare vault
```

### Deploy Vault

Create `vault` secret to hold the Vault TLS certificates:

```
cat vault.pem ca.pem > vault-combined.pem
```

```
kubectl create secret generic vault \
  --from-file=ca.pem \
  --from-file=vault.pem=vault-combined.pem \
  --from-file=vault-key.pem
```

Generate the `vault.hcl` configuration file and store it in the `vault` configmap:

```
cat > vault.hcl <<EOF
listener "tcp" {
  address = "0.0.0.0:8200"
  tls_cert_file = "/etc/vault/tls/vault.pem"
  tls_key_file = "/etc/vault/tls/vault-key.pem"
  tls_min_version = "tls12"
}

storage "gcs" {
  bucket = "${GCS_BUCKET_NAME}"
  ha_enabled = "true"
}

ui = true
EOF
```

Create the `vault` configmap:

```
kubectl create configmap vault \
  --from-file vault.hcl \
  --from-literal api-addr=https://${VAULT_LOAD_BALANCER_IP}:8200 \
  --from-literal gcs-bucket-name=${GCS_BUCKET_NAME} \
  --from-literal kms-key-id=${KMS_KEY_ID}
```

#### Create the Vault Deployments

```
kubectl apply -f vault.yaml
```
```
service "vault" created
statefulset "vault" created
```

#### Create the Vault Services

Create a directory to hold the Vault service configs:

Generate the `vault` service configuration that expose Vault using an external loadbalancer.

```
cat > vault-load-balancer.yaml <<EOF
apiVersion: v1
kind: Service
metadata:
  name: vault-load-balancer
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

Create the Vault services

```
kubectl apply -f vault-load-balancer.yaml
```

```
service "vault-load-balancer" created
```

### Initialize Vault

A [readiness probe](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-probes) is used to ensure Vault instances are not routed traffic when they are [sealed](https://www.vaultproject.io/docs/concepts/seal.html).

> Sealed Vault instances do not forward or redirect clients even in HA setups.

At this point both vault instances should running. You can now source the `vault.env` script to configure the vault CLI to use the load balancer:

```
source vault.env
```
