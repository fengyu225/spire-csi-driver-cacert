# Install SPIRE CA Certificate to Container System Trust Store using cert-manager-csi-driver-cacerts

The cert-manager-csi-driver-cacerts allows us to automatically inject CA certificates into the system's default trust store.

## Installation

### 0. Create Kubernetes cluster
```bash
kind create cluster --config kind-config.yaml
```

### 1. Deploy SPIRE

Deploy the SPIRE server and agent:

```bash
pushd spire/postgres
docker-compose up -d
popd

kubectl apply -k spire/
```

### 2. Install cert-manager-csi-driver-cacerts

```bash
kubectl apply -k csi-driver-cacerts/
```

### 3. Create Test Namespace

```bash
kubectl create namespace demo
```

### 4. Create CA Secret

Create a Secret containing the SPIRE CA bundle:

```bash
cat << EOF | kubectl apply -f -
apiVersion: v1
kind: Secret
metadata:
  name: ca
  namespace: demo
type: kubernetes.io/tls
data:
  tls.crt: $(kubectl -n spire get cm spire-bundle -o jsonpath='{.data.bundle\.crt}' | base64 | tr -d '\n')
  tls.key: $(echo "empty" | base64)
EOF
```

### 5. Deploy Test Pod

```
kubectl apply -k tests/
```

## Verification

To verify the CA certificates installation:

```bash
# Exec into the pod
kubectl exec -it curl-alpine -n demo -- sh

# Check certificates
openssl crl2pkcs7 -nocrl -certfile /etc/ssl/certs/ca-certificates.crt | \
  openssl pkcs7 -print_certs -noout | \
  grep 'example.org'
```

Expected output:
```
subject=C = US, O = Example Organization, CN = example.org, serialNumber = 15813487182433379209190809655230461912
issuer=C = US, O = Example Organization, CN = example.org, serialNumber = 15813487182433379209190809655230461912
subject=C = US, O = Example Organization, CN = example.org, serialNumber = 3756855082332013974854847939963196570
issuer=C = US, O = Example Organization, CN = example.org, serialNumber = 3756855082332013974854847939963196570
```

## References 

- [cert-manager-csi-driver-cacerts Documentation](https://github.com/cert-manager/csi-driver-cacerts)
- [SPIRE Documentation](https://spiffe.io/docs/latest/try/getting-started-k8s/)