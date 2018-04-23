# Vault on Google Kubernetes Engine

This tutorial walks you through provisioning a two node [HashiCorp Vault](https://www.vaultproject.io/intro/index.html) cluster on [Google Kubernetes Engine](https://cloud.google.com/kubernetes-engine).

## Cluster Features

* High Availability - The Vault cluster will be provisioned in [multi-server mode](https://www.vaultproject.io/docs/concepts/ha.html) for high availability.
* Google Cloud Storage Storage Backend - Vault's data is persisted in [Google Cloud Storage](https://cloud.google.com/storage).

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

### Provision a Kubernetes Cluster

```
gcloud container clusters create vault \
  --async \
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

### Deploy Vault

```
gcloud container clusters get-credentials vault
```

```
gcloud container clusters list
```
```
NAME   LOCATION    MASTER_VERSION  MASTER_IP       MACHINE_TYPE   NODE_VERSION  NUM_NODES  STATUS
vault  us-west1-c  1.9.6-gke.1     XX.XXX.XXX.XXX  n1-standard-2  1.9.6-gke.1   3          RUNNING
```

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

Create the `vault` configmap:

```
kubectl create configmap vault --from-file vault.hcl
```

Create the `vault-0` configmap which holds the loadbalancer IP that points to the `vault-0` Vault instance:

```
kubectl create configmap vault-0 \
  --from-literal api-addr=https://${VAULT_0_LOAD_BALANCER_IP}:8200
```

Create the `vault-1` configmap which holds the loadbalancer IP that points to the `vault-1` Vault instance:

```
kubectl create configmap vault-1 \
  --from-literal api-addr=https://${VAULT_1_LOAD_BALANCER_IP}:8200
```

#### Create the Vault Deployments

```
kubectl apply -f vault.yaml
```
```
deployment "vault-0" created
deployment "vault-1" created
```

#### Create the Vault Services

Create a directory to hold the Vault service configs:

```
mkdir services
```

Generate the `vault`, `vault-0`, and `vault-1` service configurations that expose the Vault instances using an external loadbalancer.

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
  publishNotReadyAddresses: true
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
  publishNotReadyAddresses: true
EOF
```

Create the Vault services

```
kubectl apply -f services
```

```
service "vault-0" created
service "vault-1" created
service "vault" created
```

### Initialize Vault

At this point both vault instances are running, but not ready:

```
kubectl get pods
```
```
NAME                       READY     STATUS    RESTARTS   AGE
vault-0-XXXXXXXXX-XXXXX    0/1       Running   0          1m
vault-1-XXXXXXXXX-XXXXX    0/1       Running   0          1m
```

A [readiness probe](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-probes) is used to ensure Vault instances are not routed traffic when they are [sealed](https://www.vaultproject.io/docs/concepts/seal.html).

> Sealed Vault instances do not forward or redirect clients even in HA setups.

```
readinessProbe:
  exec:
    command:
      - vault
      - status
      - -client-cert=/etc/vault/tls/vault.pem
      - -client-key=/etc/vault/tls/vault-key.pem
      - -ca-cert=/etc/vault/tls/ca.pem
  initialDelaySeconds: 5
  periodSeconds: 30
```


In a new terminal set up a port forward to `vault-0`:

```
kubectl port-forward $(kubectl get pods -l instance=0 \
  -o jsonpath={.items[0].metadata.name}) \
  8200:8200
```

In a seperate terminal connect to the `vault-0` instance and [initialize it](https://www.vaultproject.io/intro/getting-started/deploy.html#initializing-the-vault):

```
source vault-port-forward.env
```

```
vault operator init
```

With Vault initialized [unseal](https://www.vaultproject.io/intro/getting-started/deploy.html#seal-unseal) the `vault-0` instance:

```
vault operator unseal
```

Switch back to the terminal where `kubectl port-foward` is running and kill it

```
^C
```

Next we need to unseal the `vault-1` instance.

In a new terminal set up a port forward to `vault-1`:

```
kubectl port-forward $(kubectl get pods -l instance=1 \
  -o jsonpath={.items[0].metadata.name}) \
  8200:8200
```

In a seperate terminal unseal the `vault-1` instance:

```
source vault-port-forward.env
```

```
vault operator unseal
```

At this point both vault instances should running. You can now source the `vault.env` script to configure the vault CLI to use the load balancer:

```
source vault.env
```
