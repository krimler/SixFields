# CAPI-WRK-002 — a node is not joining the cluster

After this page you can decide whether the node never tried to join, tried and was
rejected, or joined and is not ready — and read the workload cluster directly, which is
the only place the answer exists.

## What you are seeing

```
Machine/dev-1-default-6b9c8-mn4tp — a node is not joining the cluster (7m10s)
raw: kubectl get machine.cluster.x-k8s.io dev-1-default-6b9c8-mn4tp -n default -o yaml
next: cluster docs CAPI-WRK-002
```

The `workers` row of `cluster status` reads `stalled`. This code is narrower than
`CAPI-WRK-001`: the machine's infrastructure and bootstrap data both exist, so everything
the management cluster controls has already succeeded.

## What is actually true

The `Machine` (`cluster.x-k8s.io/v1beta2`) has passed its first two steps and is stuck on
the third:

- `BootstrapConfigReady` True and `InfrastructureReady` True — the data secret exists and
  the machine was created.
- `NodeHealthy` False with reason `NodeDoesNotExist` — no Node object in the workload
  cluster matches this machine's provider ID. Nothing joined.
- `NodeHealthy` False with reason `NodeNotReady`, or `NodeReady` False with reason
  `NodeNotReady` — a Node exists and its kubelet reports not ready. Something joined and
  is unhealthy; that is usually a missing CNI.
- Reason `ConnectionDown` or `InspectionFailed` — CAPI cannot reach the workload API
  server, so it does not know either way. Fix that first.
- `status.nodeRef` is empty until a node is matched. It is the fastest thing to look at.

The `Cluster`'s `RemoteConnectionProbe` condition (reasons `ProbeSucceeded` /
`ProbeFailed`) tells you whether the management cluster can talk to the workload API at
all — the difference between "no node" and "cannot see the node".

The `KubeadmConfig` (`bootstrap.cluster.x-k8s.io/v1beta2`) holds the join instructions:
`DataSecretAvailable` True with reason `Available` means the secret was produced. The
secret's contents carry the join token and the API endpoint the node will dial. A
kubeadm bootstrap token lives 15 minutes by default; a machine created from a stale
secret is rejected at join time and the reason is only visible on the node.

## Check, in order

1. Confirm which of the three cases you are in.

   ```
   kubectl get machine.cluster.x-k8s.io <name> -n default \
     -o jsonpath='{.status.phase}{"  "}{.status.nodeRef.name}{"\n"}{range .status.conditions[*]}{.type}{"  "}{.status}{"  "}{.reason}{"  "}{.message}{"\n"}{end}'
   ```

   Good: a node name is printed and `NodeHealthy False NodeNotReady` — the node joined;
   go to step 4.
   Bad: no node name and `NodeHealthy False NodeDoesNotExist` — nothing joined; go to
   step 2.
   Also bad: reason `ConnectionDown` — go to step 3 first; the rest is unreadable until
   the probe succeeds.

2. Ask the workload cluster what it has.

   ```
   clusterctl get kubeconfig dev-1 -n default > /tmp/dev-1.kubeconfig
   kubectl --kubeconfig /tmp/dev-1.kubeconfig get nodes -o wide
   ```

   Good: the node is listed, and the Machine simply has not matched it yet — wait one
   reconcile.
   Bad: the node is absent — the join never completed. Read the machine's own log
   (step 5).

3. Check the management cluster can reach the workload API server.

   ```
   kubectl get cluster dev-1 -n default \
     -o jsonpath='{range .status.conditions[*]}{.type}{"  "}{.status}{"  "}{.reason}{"\n"}{end}' | grep RemoteConnectionProbe
   kubectl get cluster dev-1 -n default -o jsonpath='{.spec.controlPlaneEndpoint}{"\n"}'
   ```

   Good: `RemoteConnectionProbe True ProbeSucceeded` and an endpoint with a host and port.
   Bad: `ProbeFailed`. On the docker backend the load-balancer container is the usual
   cause: `docker ps -a --filter name=dev-1` and `docker logs <lb-container>`.

4. Node exists but is not ready: read the node's own conditions.

   ```
   kubectl --kubeconfig /tmp/dev-1.kubeconfig describe node <node> | sed -n '/Conditions:/,/Addresses:/p'
   kubectl --kubeconfig /tmp/dev-1.kubeconfig get pods -A -o wide | grep -v Running
   ```

   Good: only `Ready` is False and the message mentions the CNI not being initialised —
   the add-ons are the fault: `cluster docs CAPI-ADDON-001`.
   Bad: `DiskPressure` or `MemoryPressure` True — the host is out of resources.

5. Nothing joined: read the machine's log.

   ```
   docker ps -a --filter name=dev-1-default
   docker logs --tail=200 <container> 2>&1 | grep -iE "kubeadm|join|token|x509|refused"
   ```

   Good: `kubeadm join` lines that end in a successful join.
   Bad: `token is invalid or expired` — the bootstrap secret went stale; delete the
   Machine and let the pool make a new one.
   Also bad: `connection refused` or `no route to host` against the control-plane
   endpoint — the node cannot reach the API server; step 3's endpoint is wrong or the
   load balancer is down.
   Also bad: `x509` errors — the machine was created against a different cluster CA.

## Common causes

| cause | how you recognise it | fix |
|---|---|---|
| Bootstrap token expired | The machine's log says the token is invalid or expired | Delete that Machine; the pool creates a fresh one with a new token |
| No CNI in the workload cluster | Node exists, `Ready` False, kubelet message names an uninitialised network | `cluster docs CAPI-ADDON-001` |
| Node cannot reach the control-plane endpoint | Machine log shows connection refused against `spec.controlPlaneEndpoint` | Fix the load balancer container, then delete the Machine |
| Management cluster cannot reach the workload API | Cluster `RemoteConnectionProbe` reason `ProbeFailed`, Machine reason `ConnectionDown` | Same as above; the node may already be fine |
| Machine is a leftover from a previous cluster | `x509` errors in the machine log | Delete the Machine and its infrastructure object |
| Kubelet dead on the node | Node absent, machine log has kubelet crash output | Read that crash; the node image or the cgroup driver is the usual cause |

## If none of that explains it

```
cluster status dev-1 --json > /tmp/status.json
cluster why dev-1 --verbose > /tmp/why.txt
clusterctl describe cluster dev-1 -n default --show-conditions all > /tmp/tree.txt
kubectl get machine,devmachine,kubeadmconfig -n default \
  -l cluster.x-k8s.io/cluster-name=dev-1 -o yaml > /tmp/objects.yaml
clusterctl get kubeconfig dev-1 -n default > /tmp/dev-1.kubeconfig
kubectl --kubeconfig /tmp/dev-1.kubeconfig get nodes -o yaml > /tmp/nodes.yaml
docker logs --tail=500 <container> > /tmp/machine.log 2>&1
```

Never paste `/tmp/dev-1.kubeconfig` itself into a report or a model prompt: it contains
client credentials for the workload cluster. `cluster why --explain` redacts it; a manual
paste does not.
