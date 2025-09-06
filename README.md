# spale - Automatic Spot Scale Balancing


## TLS

```
# CA
openssl genpkey -algorithm ed25519 > ca.key
openssl req -x509 -new -nodes -key ca.key -sha256 -days 10000 -out ca.pem
# TLS cert
openssl genpkey -algorithm ed25519 > cert.key
openssl req -x509 -key cert.key -CA ca.pem -CAkey ca.key -sha256 -days 10000 -nodes \
    -out cert.pem -subj '/CN=spale.kube-system.svc' \
    -addext 'subjectAltName=DNS:spale.kube-system.svc,DNS:spale.kube-system.svc.cluster.local'
```