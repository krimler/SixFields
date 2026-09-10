package fixture

import (
	"fmt"
	"sort"
	"time"

	"capi-distro/internal/snapshot"
)

// Timeline is one scenario: a name, why it exists, and the envelopes a watcher
// would have written while it ran.
type Timeline struct {
	Name      string
	Note      string
	Envelopes []snapshot.Envelope
}

// All returns every synthetic scenario, keyed by name. The names match the ones
// PLAN.md Phase 3 asks for, so a recorded fixture can replace a synthetic one
// directory for directory.
func All() []Timeline {
	return []Timeline{
		stdDockerHappy(),
		stallBadVersion(),
		stallCPKilled(),
		stallBadVariable(),
		scaleUp(),
		inmemHappy(),
		inmemStallEtcd(),
		inmemStallNode(),
		hostedDockerHappy(),
		hostedStallPod(),
		twoStalls(),
	}
}

func Names() []string {
	var out []string
	for _, t := range All() {
		out = append(out, t.Name)
	}
	sort.Strings(out)
	return out
}

type opts struct {
	name        string
	note        string
	cluster     string
	placement   string // self | hosted
	backend     string // docker | inMemory
	cpReplicas  int64
	workerCount int64
	// noDerived builds only the Cluster. A topology that never reconciled has no
	// infrastructure, control plane or pool objects at all, and a fixture that
	// pretended otherwise would let the ranker pick an object that does not exist.
	noDerived bool
}

// build assembles the object graph a topology controller would have created, in
// its t+0 state. Every later step mutates it in place and snapshots.
func build(o opts) (*scenario, map[string]*object) {
	s := &scenario{name: o.name, note: o.note, cluster: o.cluster}
	objs := map[string]*object{}

	cpKind, cpGroup := "KubeadmControlPlane", "controlplane.cluster.x-k8s.io"
	if o.placement == "hosted" {
		cpKind = "K0smotronControlPlane"
	}

	cluster := s.add(newObject(coreAPI, "Cluster", o.cluster))
	cluster.spec("topology", map[string]any{
		"classRef": map[string]any{"name": "std"},
		"version":  "v1.34.11",
		"variables": []any{
			map[string]any{"name": "size", "value": "dev"},
			map[string]any{"name": "placement", "value": o.placement},
		},
		"workers": map[string]any{
			"machineDeployments": []any{
				map[string]any{"name": "default", "class": "default", "replicas": o.workerCount},
			},
		},
	})
	cluster.ref("infrastructureRef", "DevCluster", o.cluster, "infrastructure.cluster.x-k8s.io")
	cluster.ref("controlPlaneRef", cpKind, o.cluster+"-cp", cpGroup)
	cluster.status("phase", "Provisioning")
	cluster.condition("TopologyReconciled", "False", "ClusterCreating", "waiting for the cluster to be created", T0)
	cluster.condition("InfrastructureReady", "False", "NotReady", "waiting for the infrastructure provider", T0)
	cluster.condition("ControlPlaneInitialized", "False", "NotInitialized", "waiting for the control plane", T0)
	cluster.condition("ControlPlaneAvailable", "False", "NotAvailable", "waiting for the control plane", T0)
	cluster.condition("WorkersAvailable", "False", "NotAvailable", "waiting for worker machines", T0)
	cluster.status("initialization", map[string]any{"infrastructureProvisioned": false, "controlPlaneInitialized": false})
	if o.placement != "hosted" {
		// A hosted control plane has no machines, so CAPI has no replica rollup to
		// report for it; the K0smotronControlPlane's own conditions are all there is.
		cluster.status("controlPlane", map[string]any{"desiredReplicas": o.cpReplicas, "replicas": int64(0), "readyReplicas": int64(0)})
	}
	cluster.status("workers", map[string]any{"desiredReplicas": o.workerCount, "replicas": int64(0), "readyReplicas": int64(0)})
	objs["cluster"] = cluster
	if o.noDerived {
		return s, objs
	}

	infra := s.add(newObject(infraAPI, "DevCluster", o.cluster)).owned(o.cluster).ownedBy("Cluster", o.cluster)
	infra.spec("backend", backendSpec(o.backend))
	infra.condition("LoadBalancerAvailable", "False", "Provisioning", "creating the load balancer container", T0)
	infra.status("initialization", map[string]any{"provisioned": false})
	objs["infra"] = infra

	cp := s.add(newObject(cpAPI, cpKind, o.cluster+"-cp")).owned(o.cluster).ownedBy("Cluster", o.cluster)
	cp.spec("version", "v1.34.11")
	if o.placement != "hosted" {
		cp.spec("replicas", o.cpReplicas)
		cp.status("replicas", int64(0))
		cp.status("readyReplicas", int64(0))
	}
	if o.placement == "hosted" {
		cp.condition("Available", "False", "NotAvailable", "control-plane pods are not ready", T0)
	} else {
		cp.condition("Initialized", "False", "NotInitialized", "waiting for the first control-plane machine", T0)
		cp.condition("Available", "False", "NotAvailable", "waiting for the API server", T0)
		cp.condition("EtcdClusterHealthy", "Unknown", "Provisioning", "etcd has not started", T0)
	}
	objs["cp"] = cp

	md := s.add(newObject(coreAPI, "MachineDeployment", o.cluster+"-md-0")).owned(o.cluster).ownedBy("Cluster", o.cluster)
	md.spec("replicas", o.workerCount)
	md.status("replicas", int64(0))
	md.status("readyReplicas", int64(0))
	md.condition("Available", "False", "NotAvailable", "no machines are ready", T0)
	objs["md"] = md

	return s, objs
}

func backendSpec(backend string) map[string]any {
	if backend == "inMemory" {
		return map[string]any{"inMemory": map[string]any{
			"vm":        map[string]any{"provisioning": map[string]any{"startupDuration": "2s"}},
			"etcd":      map[string]any{"provisioning": map[string]any{"startupDuration": "2s"}},
			"apiServer": map[string]any{"provisioning": map[string]any{"startupDuration": "2s"}},
			"node":      map[string]any{"provisioning": map[string]any{"startupDuration": "2s"}},
		}}
	}
	return map[string]any{"docker": map[string]any{}}
}

// controlPlaneMachine adds a control-plane Machine with its infra and bootstrap
// objects, in the shape CAPI creates them.
func (s *scenario) controlPlaneMachine(cluster, cpKind, cpName, name string, at time.Time) map[string]*object {
	m := s.add(newObject(coreAPI, "Machine", name)).owned(cluster).ownedBy(cpKind, cpName)
	m.ref("infrastructureRef", "DevMachine", name, "infrastructure.cluster.x-k8s.io")
	m.ref("bootstrapRef", "KubeadmConfig", name, "bootstrap.cluster.x-k8s.io")
	m.status("phase", "Provisioning")
	m.condition("InfrastructureReady", "False", "NotReady", "waiting for the machine container", at)
	m.condition("BootstrapConfigReady", "True", "Ready", "", at)
	m.condition("NodeHealthy", "Unknown", "NodeNotFound", "the node has not registered", at)
	m.condition("Ready", "False", "NotReady", "waiting for infrastructure", at)

	dm := s.add(newObject(infraAPI, "DevMachine", name)).owned(cluster).ownedBy("Machine", name)
	dm.condition("ContainerProvisioned", "False", "Provisioning", "starting the machine container", at)
	dm.condition("BootstrapCompleted", "False", "Provisioning", "bootstrap has not run", at)

	kc := s.add(newObject(bootstrapAPI, "KubeadmConfig", name)).owned(cluster).ownedBy("Machine", name)
	kc.condition("Ready", "True", "Ready", "", at)

	return map[string]*object{"machine": m, "devmachine": dm, "config": kc}
}

func (s *scenario) workerMachine(cluster, mdName, name string, at time.Time) map[string]*object {
	m := s.add(newObject(coreAPI, "Machine", name)).owned(cluster).ownedBy("MachineSet", mdName+"-set")
	m.ref("infrastructureRef", "DevMachine", name, "infrastructure.cluster.x-k8s.io")
	m.status("phase", "Provisioning")
	m.condition("InfrastructureReady", "False", "NotReady", "waiting for the machine container", at)
	m.condition("BootstrapConfigReady", "True", "Ready", "", at)
	m.condition("NodeHealthy", "Unknown", "NodeNotFound", "the node has not registered", at)
	m.condition("Ready", "False", "NotReady", "waiting for infrastructure", at)

	dm := s.add(newObject(infraAPI, "DevMachine", name)).owned(cluster).ownedBy("Machine", name)
	dm.condition("ContainerProvisioned", "False", "Provisioning", "starting the machine container", at)
	return map[string]*object{"machine": m, "devmachine": dm}
}

// --- state transitions, so each scenario reads as a timeline rather than as a
// pile of literals ---

func infraReady(objs map[string]*object, at time.Time) {
	objs["infra"].condition("LoadBalancerAvailable", "True", "Available", "", at)
	objs["infra"].status("initialization", map[string]any{"provisioned": true})
	objs["cluster"].condition("InfrastructureReady", "True", "Ready", "", at)
	objs["cluster"].status("initialization", map[string]any{"infrastructureProvisioned": true, "controlPlaneInitialized": false})
	objs["cluster"].condition("TopologyReconciled", "True", "ReconcileSucceeded", "", at)
}

func machineReady(m map[string]*object, at time.Time) {
	m["devmachine"].condition("ContainerProvisioned", "True", "Provisioned", "", at)
	if _, ok := m["devmachine"].o["status"]; ok {
		m["devmachine"].condition("BootstrapCompleted", "True", "Completed", "", at)
	}
	m["machine"].condition("InfrastructureReady", "True", "Ready", "", at)
	m["machine"].condition("NodeHealthy", "True", "Healthy", "", at)
	m["machine"].condition("Ready", "True", "Ready", "", at)
	m["machine"].status("phase", "Running")
}

func controlPlaneUp(objs map[string]*object, replicas int64, at time.Time) {
	objs["cp"].condition("Initialized", "True", "Initialized", "", at)
	objs["cp"].condition("Available", "True", "Available", "", at)
	objs["cp"].condition("EtcdClusterHealthy", "True", "Healthy", "", at)
	objs["cp"].status("replicas", replicas)
	objs["cp"].status("readyReplicas", replicas)
	objs["cluster"].condition("ControlPlaneInitialized", "True", "Initialized", "", at)
	objs["cluster"].condition("ControlPlaneAvailable", "True", "Available", "", at)
	objs["cluster"].status("initialization", map[string]any{"infrastructureProvisioned": true, "controlPlaneInitialized": true})
	objs["cluster"].status("controlPlane", map[string]any{"desiredReplicas": replicas, "replicas": replicas, "readyReplicas": replicas})
}

func workersUp(objs map[string]*object, count int64, at time.Time) {
	objs["md"].condition("Available", "True", "Available", "", at)
	objs["md"].status("replicas", count)
	objs["md"].status("readyReplicas", count)
	objs["cluster"].condition("WorkersAvailable", "True", "Available", "", at)
	objs["cluster"].status("workers", map[string]any{"desiredReplicas": count, "replicas": count, "readyReplicas": count})
	objs["cluster"].status("phase", "Provisioned")
}

func at(seconds int) time.Time { return T0.Add(time.Duration(seconds) * time.Second) }

func snap(s *scenario, tl *Timeline, seconds int) {
	tl.Envelopes = append(tl.Envelopes, s.envelope(at(seconds)))
}

func fmtName(cluster string, i int) string { return fmt.Sprintf("%s-md-0-%d", cluster, i) }
