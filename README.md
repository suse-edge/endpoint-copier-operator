# endpoint-copier-operator

This is a Kubernetes operator whose purpose is to keep the Endpoint Slices of a Kubernetes Service in sync with another Kubernetes Service.

It is used on the SUSE Edge and SUSE Telco cloud products to expose the Kubernetes API on High Available RKE2/K3s cluster deployments.

### Why is it needed?
As explained in [kubernetes documentation - Service without selector](https://kubernetes.io/docs/concepts/services-networking/service/#services-without-selectors), when a Service API object is created without specifying any "pod selector" (i.e., `.spec.selector` stanza is not set) the kubernetes built-in endpointslices controller does not create/manage the corresponding EndpointSlice API objects for it, so that/those must be created and handled "by someone else".
This is the case for the "special" built-in `kubernetes` Service object in the `default` namespace: the (on purpose) missing "pod selector" makes the built-in endpointslices controller to ignore it.

However an special "reconciler" running inside each apiserver instance takes care of managing the corresponding `kubernetes.default` EndpointSlice API object as follow:
* the reconcilers create the corresponding `kubernetes` EndpointSlice object in the `default` namespace.
* they populate the content of that managed EndpointSlice object only with the control-plane node's IPv4/IPv6 addresses of those available apiserver instances as reported through the associated `apiserver-<xxxxx>` Lease objects in `kube-system` namespace.
* if one of those `apiserver-<xxxxx>` leases is not renewed on time by the apiserver instance holder, the other apiserver instances detect it and remove the faulty apiserver's IP address from the `kubernetes.default` EndpointSlice object.

Note that using a pod selector for the `kubernetes.default` Service object would have raised a "chicken-and-egg" situation due to using the API server to detect the availabilty of the apiserver instances itself and so a solution like that was discarded by kubernetes architects in the early days.

The same "chicken-and-egg" problem arises when aiming to expose the `kubernetes.default` endpoints outside a Kubernetes High Available cluster using a `type:LoadBalancer` Service object. An approach based on setting a "pod selector" in a defined LoadBalancer Service object that points to all the static kube-apiserver pods "running" in the control-plane nodes would fail as soon as experiencing availability/reachabilty issues in any control-plane node, as tried to be explained here below:
* A node is marked unavailable when its kubelet fails to renew its Lease object before expiration. In the Kubernetes heartbeat mechanism, each node has a dedicated Lease object in the `kube-node-lease` namespace. Each kubelet must periodically renew the assigned lease via the API server; if it fails, the built-in nodes controller detects the expired lease and updates the Node object's status to Unknown or NotReady.
* A kubelet not being able to renew its lease means it could not reach the API server before the lease timed-out for whatever reason: that kubelet daemon crashed and failed to be restarted, the apiserver instance(s) it tries to contact is (are) not reachable/availabe/running, etc.
* Let's take one of the possible causes: a networking issue left that control-plane node isolated from the rest; the kubelet running in that node should now inform to the kubernetes control plane controllers (through the API server) that the apiserver instance running in that node is not reachable, setting the status of the static kube-apiserver pod representing that apiservice instance as "Not Ready" (for the built-in endpointslices controller to detect/watch it and automatically remove that IP address from the EndpointSlice object associated to the type=LoadBalancer Service object) ... but this is not possible to happen as that kubelet instance cannot reach the API server ! Again a chicken-and-egg deadlock due to trying to use the API server to report an apiserver instance availability issue ...

To resolve these consistency issues, the _endpoint-copier-operator_ runs a dedicated reconciliation loop. Its primary job is to mirror the built-in `kubernetes.default` EndpointSlice object's list of endpoints into a managed `kubernetes-vip.default` EndpointSlice object. This managed EndpointSlice is linked to a manually created `kubernetes-vip.default` LoadBalancer Service object, providing a reliable bridge between the internal cluster discovery and the external load balancing infrastructure.

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
