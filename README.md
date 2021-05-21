# Vault on Google Kubernetes Engine

This tutorial walks you through provisioning a multi-node [HashiCorp Vault](https://www.vaultproject.io) cluster on [Google Kubernetes Engine](https://cloud.google.com/kubernetes-engine).

## Cluster Features

* High Availability - The Vault cluster will be provisioned in [multi-server mode](https://www.vaultproject.io/docs/concepts/ha.html) for high availability.
* Google Cloud Storage Storage Backend - Vault's data is persisted in [Google Cloud Storage](https://cloud.google.com/storage).
* Production Hardening - Vault is configured and deployed based on the guidance found in the [production hardening](https://www.vaultproject.io/guides/operations/production.html) guide.
* Auto Initialization and Unsealing - Vault is automatically initialized and unsealed at runtime. Keys are encrypted using [Cloud KMS](https://cloud.google.com/kms) and stored on [Google Cloud Storage](https://cloud.google.com/storage).

## Tutorial

### Create a New Project

In this section you will create a new GCP project and enable the APIs required by this tutorial.

Generate a project ID:

```
PROJECT_ID="vault-$(($(date +%s%d)/1000000))"
```

Create a new GCP project:

```
gcloud projects create ${PROJECT_ID} \
  --name "${PROJECT_ID}"
```

[Enable billing](https://cloud.google.com/billing/docs/how-to/modify-project#enable_billing_for_a_new_project) on the new project before moving on to the next step.

Enable the GCP APIs required by this tutorial:

```
gcloud services enable \
  cloudapis.googleapis.com \
  cloudkms.googleapis.com \
  container.googleapis.com \
  containerregistry.googleapis.com \
  iam.googleapis.com \
  --project ${PROJECT_ID}
```

### Set Configuration

```
COMPUTE_ZONE="us-west1-c"
```

```
COMPUTE_REGION="us-west1"
```

```
GCS_BUCKET_NAME="${PROJECT_ID}-vault-storage"
```

```
KMS_KEY_ID="projects/${PROJECT_ID}/locations/global/keyRings/vault/cryptoKeys/vault-init"
```

### Create KMS Keyring and Crypto Key

In this section you will create a Cloud KMS [keyring](https://cloud.google.com/kms/docs/object-hierarchy#key_ring) and [cryptographic key](https://cloud.google.com/kms/docs/object-hierarchy#key) suitable for encrypting and decrypting Vault [master keys](https://www.vaultproject.io/docs/concepts/seal.html) and [root tokens](https://www.vaultproject.io/docs/concepts/tokens.html#root-tokens). 

Create the `vault` kms keyring:

```
gcloud kms keyrings create vault \
  --location global \
  --project ${PROJECT_ID}
```

Create the `vault-init` encryption key:

```
gcloud kms keys create vault-init \
  --location global \
  --keyring vault \
  --purpose encryption \
  --project ${PROJECT_ID}
```

### Create a Google Cloud Storage Bucket

Google Cloud Storage is used to [persist Vault's data](https://www.vaultproject.io/docs/configuration/storage/google-cloud-storage.html) and hold encrypted Vault master keys and root tokens.

Create a GCS bucket:

```
gsutil mb -p ${PROJECT_ID} gs://${GCS_BUCKET_NAME}
```

### Create the Vault IAM Service Account

An [IAM service account](https://cloud.google.com/iam/docs/service-accounts) is used by Vault to access the GCS bucket and KMS encryption key created in the previous sections.

Create the `vault` service account:

```
gcloud iam service-accounts create vault-server \
  --display-name "vault service account" \
  --project ${PROJECT_ID}
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

Grant access to the `vault-init` KMS encryption key:

```
gcloud kms keys add-iam-policy-binding \
  vault-init \
  --location global \
  --keyring vault \
  --member serviceAccount:vault-server@${PROJECT_ID}.iam.gserviceaccount.com \
  --role roles/cloudkms.cryptoKeyEncrypterDecrypter \
  --project ${PROJECT_ID}
```

### Provision a Kubernetes Cluster

In this section you will provision a three node Kubernetes cluster using [Google Kubernetes Engine](https://cloud.google.com/kubernetes-engine) with access to the `vault-server` service account across the entire cluster.

Create the `vault` Kubernetes cluster:

```
gcloud container clusters create vault \
  --enable-autorepair \
  --machine-type e2-standard-2 \
  --service-account vault-server@${PROJECT_ID}.iam.gserviceaccount.com \
  --num-nodes 3 \
  --zone ${COMPUTE_ZONE} \
  --project ${PROJECT_ID}
```

> Warning: Each node in the `vault` Kubernetes cluster has access to the `vault-server` service account. The `vault` cluster should only be used for running Vault. Other workloads should run on a different cluster and access Vault through an internal or external load balancer. 


### Provision IP Address

In this section you will create a public IP address that will be used to expose the Vault server to external clients.

Create the `vault` compute address:

```
gcloud compute addresses create vault \
  --region ${COMPUTE_REGION} \
  --project ${PROJECT_ID}
```

Store the `vault` compute address in an environment variable:

```
VAULT_LOAD_BALANCER_IP=$(gcloud compute addresses describe vault \
  --region ${COMPUTE_REGION} \
  --project ${PROJECT_ID} \
  --format='value(address)')
```

### Generate TLS Certificates

In this section you will generate the self-signed TLS certificates used to secure communication between Vault clients and servers. 

Create a Certificate Authority:

```
cfssl gencert -initca ca-csr.json | cfssljson -bare ca
```

Generate the Vault TLS certificates:

```
cfssl gencert \
  -ca=ca.pem \
  -ca-key=ca-key.pem \
  -config=ca-config.json \
  -hostname="vault,vault.default.svc.cluster.local,localhost,127.0.0.1,${VAULT_LOAD_BALANCER_IP}" \
  -profile=default \
  vault-csr.json | cfssljson -bare vault
```

---

Another option is using the [Kubernetes certificates API](https://kubernetes.io/docs/tasks/tls/managing-tls-in-a-cluster/#create-a-certificate-signing-request):

```
cat <<EOF | cfssl genkey - | cfssljson -bare vault
{
  "hosts": [
    "vault",
    "vault.default.svc.cluster.local",
    "localhost",
    "127.0.0.1",
    "${VAULT_LOAD_BALANCER_IP}"
  ],
  "CN": "vault-server",
  "key": {
    "algo": "rsa",
    "size": 2048
  }
}
EOF

cat <<EOF | kubectl create -f -
apiVersion: certificates.k8s.io/v1beta1
kind: CertificateSigningRequest
metadata:
  name: vault-server
spec:
  groups:
  - system:authenticated
  request: $(cat vault.csr | base64 | tr -d '\n')
  usages:
  - digital signature
  - key encipherment
  - server auth
  - client auth
EOF

kubectl certificate approve vault-server
kubectl get csr vault-server -o jsonpath='{.status.certificate}' |base64 --decode > vault.pem

# Fetch the CA certificate of the GKE Kubernetes Cluster
gcloud container clusters describe vault --zone=${COMPUTE_ZONE} --format='value(masterAuth.clusterCaCertificate)' |base64 --decode > ca.pem
```

### Deploy Vault

In this section you will deploy the multi-node Vault cluster using a collection of Kubernetes and application configuration files.

Create the `vault` secret to hold the Vault TLS certificates:

```
cat vault.pem ca.pem > vault-combined.pem
```

```
kubectl create secret generic vault \
  --from-file=ca.pem \
  --from-file=vault.pem=vault-combined.pem \
  --from-file=vault-key.pem
```

The `vault` configmap holds the Google Cloud Platform settings required bootstrap the Vault cluster.

Create the `vault` configmap:

```
kubectl create configmap vault \
  --from-literal api-addr=https://${VAULT_LOAD_BALANCER_IP}:8200 \
  --from-literal gcs-bucket-name=${GCS_BUCKET_NAME} \
  --from-literal kms-key-id=${KMS_KEY_ID}
```

#### Create the Vault StatefulSet

In this section you will create the `vault` statefulset used to provision and manage two Vault server instances.

Create the `vault` statefulset:

```
kubectl apply -f vault.yaml
```
```
service "vault" created
statefulset "vault" created
```

At this point the multi-node cluster is up and running:

```
kubectl get pods
```
```
NAME      READY     STATUS    RESTARTS   AGE
vault-0   2/2       Running   0          1m
vault-1   2/2       Running   0          1m
```

### Automatic Initialization and Unsealing

In a typical deployment Vault must be initialized and unsealed before it can be used. In our deployment we are using the [vault-init](https://github.com/kelseyhightower/vault-init) container to automate the initialization and unseal steps.

```
kubectl logs vault-0 -c vault-init
```
```
2018/11/03 22:37:35 Starting the vault-init service...
2018/11/03 22:37:35 Get https://127.0.0.1:8200/v1/sys/health: dial tcp 127.0.0.1:8200: connect: connection refused
2018/11/03 22:37:45 Vault is not initialized. Initializing and unsealing...
2018/11/03 22:37:53 Encrypting unseal keys and the root token...
2018/11/03 22:37:53 Unseal keys written to gs://vault-1541283682815-vault-storage/unseal-keys.json.enc
2018/11/03 22:37:53 Root token written to gs://vault-1541283682815-vault-storage/root-token.enc
2018/11/03 22:37:53 Initialization complete.
2018/11/03 22:37:55 Unseal complete.
2018/11/03 22:37:55 Next check in 10s
2018/11/03 22:38:05 Vault is initialized and unsealed.
2018/11/03 22:38:05 Next check in 10s
```

The `vault-init` container runs every 10 seconds and ensures each vault instance is automatically unsealed.

#### Health Checks

A [readiness probe](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-probes) is used to ensure Vault instances are not routed traffic when they are [sealed](https://www.vaultproject.io/docs/concepts/seal.html).

> Sealed Vault instances do not forward or redirect clients even in HA setups.

### Expose the Vault Cluster

In this section you will expose the Vault cluster using an external network load balancer.

Generate the `vault` service configuration:

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

Create the `vault-load-balancer` service:

```
kubectl apply -f vault-load-balancer.yaml
```

Wait until the `EXTERNAL-IP` is populated:

```
kubectl get svc vault-load-balancer
```
```
NAME                  TYPE           CLUSTER-IP      EXTERNAL-IP   PORT(S)
vault-load-balancer   LoadBalancer   XX.XX.XXX.XXX   <pending>     8200:31805/TCP,8201:32754/TCP
```

### Smoke Tests

Source the `vault.env` script to configure the vault CLI to use the Vault cluster via the external load balancer:

```
source vault.env
```

Get the status of the Vault cluster:

```
vault status
```
```
Key                    Value
---                    -----
Seal Type              shamir
Initialized            true
Sealed                 false
Total Shares           5
Threshold              3
Version                0.11.4
Cluster Name           vault-cluster-46821b83
Cluster ID             dcd56552-27d0-fa18-4ccc-25b252464971
HA Enabled             true
HA Cluster             https://XX.XX.X.X:8201
HA Mode                standby
Active Node Address    https://XX.XXX.XXX.XX:8200
```

#### Logging in

Download and decrypt the root token:

```
export VAULT_TOKEN=$(gsutil cat gs://${GCS_BUCKET_NAME}/root-token.enc | \
  base64 --decode | \
  gcloud kms decrypt \
    --project ${PROJECT_ID} \
    --location global \
    --keyring vault \
    --key vault-init \
    --ciphertext-file - \
    --plaintext-file - 
)
```

#### Working with Secrets

The following examples assume Vault 0.11 or later.

```
vault secrets enable -version=2 kv
```

```
vault kv enable-versioning secret/
```

```
vault kv put secret/my-secret my-value=s3cr3t
```

```
vault kv get secret/my-secret
```

### Clean Up

Ensure you are working with the right project ID:

```
echo $PROJECT_ID
```

Delete the project:

```
gcloud projects delete ${PROJECT_ID}
```
