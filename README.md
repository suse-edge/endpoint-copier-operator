# endpoint-copier-operator
This is a Kubernetes operator whose purpose is to keep the Endpoint Slices of a Kubernetes Service in sync with another Kubernetes Service.

This is being used on the SUSE Edge product to expose the Kubernetes API on High Availability scenarios.

## Getting Started
You’ll need a Kubernetes cluster to run against. You can use [KIND](https://sigs.k8s.io/kind) to get a local cluster for testing, or run against a remote cluster.
**Note:** Your controller will automatically use the current context in your kubeconfig file (i.e. whatever cluster `kubectl cluster-info` shows).

### Running on the cluster
Deploy the controller to the cluster:

```sh
helm repo add suse-edge https://suse-edge.github.io/charts
helm install --create-namespace -n endpoint-copier-operator endpoint-copier-operator suse-edge/endpoint-copier-operator
```

Create a Kubernetes Service:

```sh
cat <<-EOF | kubectl apply -f -
apiVersion: v1
kind: Service
metadata:
  name: kubernetes-vip
  namespace: default
  annotations:
    endpoint-copier/enabled: "true"
    endpoint-copier/default-service-name: "kubernetes"
    endpoint-copier/default-service-namespace: "default"
spec:
  internalTrafficPolicy: Cluster
  ipFamilies:
  - IPv4
  ipFamilyPolicy: SingleStack
  ports:
  - name: rke2-api
    port: 9345
    protocol: TCP
    targetPort: 9345
  - name: k8s-api
    port: 6443
    protocol: TCP
    targetPort: 6443
  sessionAffinity: None
  type: LoadBalancer
EOF
```

### Uninstall controller
Uninstall the controller from the cluster:

```sh
helm -n endpoint-copier-operator uninstall endpoint-copier-operator
```

### How it works
This project aims to follow the Kubernetes [Operator pattern](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/).

It uses [Controllers](https://kubernetes.io/docs/concepts/architecture/controller/),
which provide a reconcile function responsible for synchronizing resources until the desired state is reached on the cluster.

### Test It Out
1. Run your controller (this will run in the foreground, so switch to a new terminal if you want to leave it running):

```sh
make run
```

**NOTE:** Run `make help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

## How SUSE Edge uses endpoint-copier-operator

The first SUSE Edge RKE2/K3s control plane node is deployed with an extra `--tls-san` [parameter](https://docs.rke2.io/reference/server_config#listener) for an extra IP (and "hostname") that will be used to expose the Kubernetes API. That parameter instructs RKE2/K3s to create the Kubernetes API certificates with that extra IP and hostname. To be able to deploy [MetalLB](https://metallb.io/) to perform the load balancing, the default 'servicelb' service is [disabled](https://metallb.io/configuration/k3s/).

Then MetalLB is deployed as well as an `IPAddressPool` and the corresponding `L2Advertisement` objects (or the `BGPAdvertisment`) for the K8s VIP.

RKE2/K3s default `kubernetes` service endpoints are the 'Ready' control plane nodes IPs, so an extra `kubernetes-vip` service (type: loadbalancer) is created to behave just like the default kubernetes service does. E-C-O keeps in sync the `kubernetes-vip` `EndpointSlices` with the default `kubernetes` service. In the event of a control-plane node going down, it goes down on both `kubernetes` and `kubernetes-vip` services, so it is _out_ of the load balancing procedure. Same if a new control-plane node goes up, it will be reflected as well.

Note: All the required objects and settings are automatically performed via combustion at installation time via [edge-image-builder](https://github.com/suse-edge/edge-image-builder/) when adding >1 hosts to the [Kubernetes section of the EIB configuration file](https://github.com/suse-edge/edge-image-builder/blob/main/docs/building-images.md#kubernetes).

## License

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
