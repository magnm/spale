# spale - Automatic Spot Scale Balancing

## Annotations

Annotations can be placed on Deployments:

- `spale/ignore: true` - Ignore this deployment entirely
- `spale/opt-in: true` - Opt-in this deployment regardless of selectors
- `spale/ratio: "3:1"` - The ratio of spot to normal pods
- `spale/node-labels: "node-role.kubernetes.io/spot: true"` - Node labels to apply to spot pods, in addition to the default ones.
- `spale/tolerations: "node-role.kubernetes.io/spot: NoSchedule"` - Tolerations to apply to spot pods, in addition to the default ones.


## Development
#### TLS

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