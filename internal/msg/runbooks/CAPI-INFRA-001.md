# CAPI-INFRA-001 — infrastructure is not ready

After this page you can name the infrastructure object that is blocking the cluster,
say whether it is still working or actually stuck, and fix the five things that cause
this most of the time.

## What you are seeing

```
DevCluster/dev-1 — infrastructure is not ready (4m12s)
  raw: kubectl get devcluster.infrastructure.cluster.x-k8s.io dev-1 -n default -o yaml
  Next: cluster docs CAPI-INFRA-001
```

The `infrastructure` row of `cluster status` reads `stalled`. Every row below it reads
`pending`: the control plane does not start until infrastructure is ready.

## What is actually true

The `Cluster` (`cluster.x-k8s.io/v1beta2`) holds the rollup. Its `InfrastructureReady`
condition is False, and the reason says what kind of failure it is:

- `NotReady` — the infrastructure object exists and reports it is not ready. The real
  message is on that object, not here.
- `ObjectDoesNotExist` — `spec.infrastructureRef` points at an object that is not there.
  That is a topology problem, not an infrastructure one: read `cluster docs CAPI-TOPO-001`.
- `InvalidConditionReported` / `InternalError` — the provider reported something CAPI
  could not read. The provider controller log is the only place with more.

The object named by `spec.infrastructureRef` holds the truth. On CAPD that is a
`DevCluster` (`infrastructure.cluster.x-k8s.io/v1beta2`):

- `Ready` — False with reason `NotReady` for the whole time the backend is provisioning.
- `LoadBalancerAvailable` — set only with `spec.backend.docker`. False with reason
  `NotAvailable` while the load-balancer container is being created; its message carries
  the container runtime's own error.

With `spec.backend.inMemory` there is no load balancer and no container: `Ready` is the
only condition, and it flips on a timer.

## Check, in order

1. Name the blocking object.

   ```
   cluster why dev-1
   ```

   Good: it names a `DevCluster` and the elapsed time is under a minute — the provider is
   still working, wait.
   Bad: it names the `Cluster` itself with reason `ObjectDoesNotExist` — no infrastructure
   object was ever created. Stop here and read `cluster docs CAPI-TOPO-001`.

2. Read the Cluster's rollup.

   ```
   kubectl get cluster dev-1 -n default \
     -o jsonpath='{range .status.conditions[*]}{.type}{"  "}{.status}{"  "}{.reason}{"  "}{.message}{"\n"}{end}'
   ```

   Good: `InfrastructureReady  False  NotReady` with a message naming the DevCluster.
   Bad: `Paused  True` — the cluster is paused and nothing reconciles at all.

3. Read the infrastructure object.

   ```
   kubectl get devcluster.infrastructure.cluster.x-k8s.io dev-1 -n default \
     -o jsonpath='{range .status.conditions[*]}{.type}{"  "}{.status}{"  "}{.reason}{"  "}{.message}{"\n"}{end}'
   ```

   Good: `Ready  False  NotReady` with a `lastTransitionTime` that moved in the last minute.
   Bad: `LoadBalancerAvailable  False  NotAvailable` with a message from the container
   runtime (port in use, no such image, permission denied) — that message is the fault.
   Also bad: no conditions at all. The provider controller never reconciled the object;
   go to step 4.

4. Confirm the infrastructure provider is running and reconciling this cluster.

   ```
   kubectl get deployments -A -l cluster.x-k8s.io/provider
   kubectl logs -n <namespace> deployment/<name> --since=15m | grep dev-1
   ```

   Take `<namespace>` and `<name>` from the first command's output.
   Good: log lines mentioning `dev-1` in the last minute.
   Bad: no deployment for the infrastructure provider — the provider is not installed, run
   `make dev-up`. Or the pod is `CrashLoopBackOff` — read its log, not the Cluster's.

5. Docker backend only: confirm the containers exist.

   ```
   docker ps -a --filter name=dev-1
   ```

   Good: a container whose name starts with `dev-1` and whose status is `Up`.
   Bad: `docker` itself fails — the container runtime is down, run `make doctor`. Or the
   container exists with status `Exited`; `docker logs <container>` has the reason.

## Common causes

| cause | how you recognise it | fix |
|---|---|---|
| Container runtime not running | `docker ps` fails; the DevCluster has no conditions | Start Docker Desktop, OrbStack or Colima, then `make doctor` |
| Cluster is paused | Cluster `Paused` condition True, or the annotation `cluster.x-k8s.io/paused` is set | `kubectl annotate cluster dev-1 -n default cluster.x-k8s.io/paused-` |
| Load-balancer container will not start | DevCluster `LoadBalancerAvailable` False, reason `NotAvailable`, runtime error in the message | Free the port or remove the dead container named in the message, then delete the DevCluster's container so CAPD recreates it |
| `spec.infrastructureRef` points at nothing | Cluster `InfrastructureReady` reason `ObjectDoesNotExist` | `cluster docs CAPI-TOPO-001` |
| Infrastructure provider not installed | No deployment carries `cluster.x-k8s.io/provider` for the infra kind | `make dev-up` |
| Provider controller crashing | Provider pod restarts climbing; its log has the panic | Fix what the log names; the Cluster conditions will never say more than `NotReady` |

## If none of that explains it

Capture this before asking anyone:

```
cluster status dev-1 --json > /tmp/status.json
cluster why dev-1 --verbose > /tmp/why.txt
clusterctl describe cluster dev-1 -n default --show-conditions all > /tmp/tree.txt
kubectl get cluster,devcluster -n default -o yaml > /tmp/objects.yaml
kubectl get deployments -A -l cluster.x-k8s.io/provider -o wide > /tmp/providers.txt
kubectl logs -n <namespace> deployment/<name> --since=30m > /tmp/provider.log
```

Add `docker ps -a --filter name=dev-1` and `docker logs <container>` on the docker
backend. `make record-fixture NAME=stall-infra CLUSTER_NAME=dev-1` turns the same state
into a fixture the ranker can be tested against.
