# Install SPIRE CA Certificate to Container System Trust Store using cert-manager-csi-driver-cacerts

## Overview

- SPIRE issues workload X.509 SVID
- spiffe-helper sidecar request and refresh certificates from SPIRE
- Golang workloads reloads TLS certificate at runtime, enabling zero-downtime certificate updates
- CSI driver for mounting CA certificates into workload trust stores
- Mutating webhook for automatic CSI volume injection

## SPIRE Certificate Flow
```
SPIRE Server
↓ (issues X.509-SVID)
SPIRE Agent
↓ (workload attestation)
spiffe-helper
↓ (writes to files)
Certificate Files (svid.pem, key.pem, bundle.pem)
↓ (file system events)
Application (this example)
↓ (updates TLS config)
TLS Connections
```

## Certificate Files

spiffe-helper manages these files:

- `svid.pem`: The X.509-SVID certificate
- `svid_key.pem`: The private key
- `svid_bundle.pem`: The trust bundle containing the X.509 root certificates

## CSI Driver for CA Certificates

The `csi-driver-cacerts` component automates CA certificate distribution in Kubernetes clusters by:
1. Mounting CA certificates into container OS default trust stores
2. Supporting automatic CA certificate updates
3. Managing CA trust bundles across namespaces

### Architecture

```
SPIRE Server
↓ (issues CA certificate)
Secret (in spire namespace)
↓ (referenced by)
CAProviderClass
↓ (used by)
CSI Driver
↓ (mounts to)
Container OS default trust store
```

### Components
1. **CSI Driver**: Mounts CA certificates into pod trust stores
2. **CAProviderClass**: Specifies which CA certificates to trust
3. **Mutating Webhook**: Automatically injects CSI volume mounts into pods

### Mutating Webhook

The webhook automatically injects CSI volume mounts into pods with the `spiffe.io/spire-managed-identity: "true"` label. It:

1. Watches pod creation events
2. Injects CSI volume and mount configurations
3. Uses SPIRE-issued certificates for TLS
4. Creates CAProviderClass resources in new namespaces

#### Configuration

1. Deploy the webhook:
```bash
cd webhook
kubectl apply -k webhook/manifests
```

## Example Workloads Setup

The project includes example client and server workloads to demonstrate the certificate management:

### Setup

```bash
kubectl apply -k workloads/manifests
```

The client can be configured in two ways:

1. Using trust store from SPIRE directly:
```yaml
spec:
  template:
    metadata:
      labels:
        app: client
        spiffe.io/spire-managed-identity: "true"
    spec:
      containers:
        - name: client
          args:
            - --cert-dir=/run/spiffe/certs
            - --system-certs=false
          volumeMounts:
            - name: spiffe-certs
              mountPath: /run/spiffe/certs
```

2. Using system trust store with CSI driver:
```yaml
spec:
  template:
    metadata:
      labels:
        app: client
        spiffe.io/spire-managed-identity: "true"
    spec:
      containers:
        - name: client
          args:
            - --cert-dir=/run/spiffe/certs
            - --system-certs=true
          volumeMounts:
            - name: cacerts
              mountPath: /etc/ssl/certs
```

### Testing the Setup

1. Deploy SPIRE components:
```bash
cd spire
kubectl apply -k .
```

2. Deploy CSI driver and webhook:
```bash
cd csi-driver-cacerts
kubectl apply -k .
```

3. Deploy example workloads:
```bash
cd workloads
kubectl apply -k .
```

4. Check client logs for certificate information:
```bash
kubectl logs -l app=client -n spiffe-demo -f
```

Example output:
```
2025/01/23 16:43:09 Certificate Information:
2025/01/23 16:43:09   Subject: CN=server-service, O=SPIRE, C=US
2025/01/23 16:43:09   Not Before: 2025-01-23T16:42:55Z
2025/01/23 16:43:09   Not After: 2025-01-23T16:43:15Z
2025/01/23 16:43:09   Time until expiration: 5s
```