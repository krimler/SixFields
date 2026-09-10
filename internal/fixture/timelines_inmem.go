package fixture

// The in-memory backend fakes VM, etcd, API server and node provisioning inside
// the management cluster's API server, with a configurable startup duration per
// component (docs/api-snapshot.md, Notes). That is what makes a stall inducible
// without Docker: set one component's duration past stallAfter and nothing else
// changes. These are the primary fixtures for `why` ranking and ETA math.

func inmemDevMachineConditions(m map[string]*object, seconds int) {
	dm := m["devmachine"]
	dm.condition("VMProvisioned", "False", "Provisioning", "waiting for the fake VM", at(seconds))
	dm.condition("EtcdProvisioned", "False", "Provisioning", "waiting for etcd", at(seconds))
	dm.condition("APIServerProvisioned", "False", "Provisioning", "waiting for the API server", at(seconds))
	dm.condition("NodeProvisioned", "False", "Provisioning", "waiting for the node", at(seconds))
}

func inmemHappy() Timeline {
	const note = "stands in for a recorded run of the std-inmemory overlay: same four phases as " +
		"the docker path but seconds instead of minutes, and DevMachine reports the four " +
		"in-memory provisioning conditions"
	s, objs := build(opts{name: "inmem-happy", note: note, cluster: "inmem-1",
		placement: "self", backend: "inMemory", cpReplicas: 1, workerCount: 2})
	tl := Timeline{Name: s.name, Note: note}

	snap(s, &tl, 0)

	cpM := s.controlPlaneMachine("inmem-1", "inmem-1-cp", "inmem-1-cp-abcde", at(2))
	inmemDevMachineConditions(cpM, 2)
	infraReady(objs, at(3))
	snap(s, &tl, 4)

	dm := cpM["devmachine"]
	dm.condition("VMProvisioned", "True", "Provisioned", "", at(6))
	dm.condition("EtcdProvisioned", "True", "Provisioned", "", at(8))
	dm.condition("APIServerProvisioned", "True", "Provisioned", "", at(10))
	dm.condition("NodeProvisioned", "True", "Provisioned", "", at(12))
	machineReady(cpM, at(12))
	controlPlaneUp(objs, at(13))
	snap(s, &tl, 14)

	w0 := s.workerMachine("inmem-1", "inmem-1-md-0", fmtName("inmem-1", 0), at(15))
	w1 := s.workerMachine("inmem-1", "inmem-1-md-0", fmtName("inmem-1", 1), at(15))
	inmemDevMachineConditions(w0, 15)
	inmemDevMachineConditions(w1, 15)
	objs["md"].status("replicas", int64(2))
	snap(s, &tl, 16)

	for _, m := range []map[string]*object{w0, w1} {
		m["devmachine"].condition("VMProvisioned", "True", "Provisioned", "", at(20))
		m["devmachine"].condition("NodeProvisioned", "True", "Provisioned", "", at(22))
		machineReady(m, at(22))
	}
	workersUp(objs, 2, at(23))
	snap(s, &tl, 24)

	return tl
}

// inmemStallEtcd is the deterministic stall PLAN.md asks for: etcd's startup
// duration is set past stallAfter, so exactly one condition stays False and
// nothing else moves. The ranker must name the DevMachine, not the Cluster.
func inmemStallEtcd() Timeline {
	const note = "hand-built from the inMemory backend: etcd startupDuration set past stallAfter, " +
		"so EtcdProvisioned stays False on one DevMachine and nothing transitions after t+8"
	s, objs := build(opts{name: "inmem-stall-etcd", note: note, cluster: "inmem-1",
		placement: "self", backend: "inMemory", cpReplicas: 1, workerCount: 2})
	tl := Timeline{Name: s.name, Note: note}

	objs["infra"].spec("backend", map[string]any{"inMemory": map[string]any{
		"vm":        map[string]any{"provisioning": map[string]any{"startupDuration": "2s"}},
		"etcd":      map[string]any{"provisioning": map[string]any{"startupDuration": "30m"}},
		"apiServer": map[string]any{"provisioning": map[string]any{"startupDuration": "2s"}},
		"node":      map[string]any{"provisioning": map[string]any{"startupDuration": "2s"}},
	}})
	cpM := s.controlPlaneMachine("inmem-1", "inmem-1-cp", "inmem-1-cp-abcde", at(2))
	inmemDevMachineConditions(cpM, 2)
	infraReady(objs, at(3))
	cpM["devmachine"].condition("VMProvisioned", "True", "Provisioned", "", at(6))
	cpM["devmachine"].condition("EtcdProvisioned", "False", "Provisioning",
		"etcd has not finished starting after 30m0s", at(8))
	objs["cluster"].status("controlPlane", map[string]any{"desiredReplicas": int64(1), "replicas": int64(1), "readyReplicas": int64(0)})
	snap(s, &tl, 10)
	snap(s, &tl, 200)
	snap(s, &tl, 400)

	return tl
}

// inmemStallNode is the same shape one component later: the API server is up but
// the node never registers, which is the classic "cluster looks fine, nothing
// schedules" failure.
func inmemStallNode() Timeline {
	const note = "hand-built from the inMemory backend: node startupDuration set past stallAfter, " +
		"so the API server is provisioned but NodeProvisioned stays False"
	s, objs := build(opts{name: "inmem-stall-node", note: note, cluster: "inmem-1",
		placement: "self", backend: "inMemory", cpReplicas: 1, workerCount: 2})
	tl := Timeline{Name: s.name, Note: note}

	objs["infra"].spec("backend", map[string]any{"inMemory": map[string]any{
		"vm":        map[string]any{"provisioning": map[string]any{"startupDuration": "2s"}},
		"etcd":      map[string]any{"provisioning": map[string]any{"startupDuration": "2s"}},
		"apiServer": map[string]any{"provisioning": map[string]any{"startupDuration": "2s"}},
		"node":      map[string]any{"provisioning": map[string]any{"startupDuration": "30m"}},
	}})
	cpM := s.controlPlaneMachine("inmem-1", "inmem-1-cp", "inmem-1-cp-abcde", at(2))
	inmemDevMachineConditions(cpM, 2)
	infraReady(objs, at(3))
	dm := cpM["devmachine"]
	dm.condition("VMProvisioned", "True", "Provisioned", "", at(6))
	dm.condition("EtcdProvisioned", "True", "Provisioned", "", at(8))
	dm.condition("APIServerProvisioned", "True", "Provisioned", "", at(10))
	dm.condition("NodeProvisioned", "False", "Provisioning",
		"the node has not registered after 30m0s", at(10))
	cpM["machine"].condition("NodeHealthy", "False", "NodeNotFound", "the node has not registered", at(10))
	objs["cp"].condition("Initialized", "True", "Initialized", "", at(10))
	objs["cluster"].condition("ControlPlaneInitialized", "True", "Initialized", "", at(10))
	objs["cluster"].status("controlPlane", map[string]any{"desiredReplicas": int64(1), "replicas": int64(1), "readyReplicas": int64(0)})
	snap(s, &tl, 12)
	snap(s, &tl, 200)
	snap(s, &tl, 400)

	return tl
}

func hostedDockerHappy() Timeline {
	const note = "stands in for placement: hosted — a k0smotron control plane runs as pods in the " +
		"management cluster, so the control-plane phase reports readiness, not node counts"
	s, objs := build(opts{name: "hosted-docker-happy", note: note, cluster: "hosted-1",
		placement: "hosted", backend: "docker", cpReplicas: 1, workerCount: 2})
	tl := Timeline{Name: s.name, Note: note}

	snap(s, &tl, 0)
	infraReady(objs, at(40))
	snap(s, &tl, 60)

	objs["cp"].condition("Available", "True", "Available", "", at(90))
	objs["cluster"].condition("ControlPlaneInitialized", "True", "Initialized", "", at(90))
	objs["cluster"].condition("ControlPlaneAvailable", "True", "Available", "", at(90))
	objs["cluster"].status("initialization", map[string]any{"infrastructureProvisioned": true, "controlPlaneInitialized": true})
	w0 := s.workerMachine("hosted-1", "hosted-1-md-0", fmtName("hosted-1", 0), at(95))
	w1 := s.workerMachine("hosted-1", "hosted-1-md-0", fmtName("hosted-1", 1), at(95))
	objs["md"].status("replicas", int64(2))
	snap(s, &tl, 100)

	machineReady(w0, at(160))
	machineReady(w1, at(165))
	workersUp(objs, 2, at(170))
	snap(s, &tl, 180)

	return tl
}

func hostedStallPod() Timeline {
	const note = "hand-built: the hosted control-plane pods never become ready, so the phase " +
		"names the K0smotronControlPlane rather than any machine"
	s, objs := build(opts{name: "hosted-stall-pod", note: note, cluster: "hosted-1",
		placement: "hosted", backend: "docker", cpReplicas: 1, workerCount: 2})
	tl := Timeline{Name: s.name, Note: note}

	infraReady(objs, at(40))
	objs["cp"].condition("Available", "False", "PodNotReady",
		"0 of 1 control-plane pods are ready: ImagePullBackOff", at(60))
	snap(s, &tl, 70)
	snap(s, &tl, 250)
	snap(s, &tl, 430)

	return tl
}

// twoStalls locks the ranking order. It is deliberately hand-made: two objects are
// stalled at once, at different depths and different ages, and only one of them is
// the right answer.
func twoStalls() Timeline {
	const note = "hand-made ranking variant: a Machine and the Cluster are both False at once. " +
		"The Machine is more specific and transitioned more recently, so it must rank first; " +
		"the Cluster's TopologyReconciled is older and broader"
	s, objs := build(opts{name: "two-stalls", note: note, cluster: "dev-1",
		placement: "self", backend: "docker", cpReplicas: 1, workerCount: 2})
	tl := Timeline{Name: s.name, Note: note}

	infraReady(objs, at(30))
	objs["cluster"].condition("TopologyReconciled", "False", "ReconcileFailed",
		"the class could not compute the control plane", at(40))
	cpM := s.controlPlaneMachine("dev-1", "dev-1-cp", "dev-1-cp-abcde", at(50))
	cpM["machine"].condition("InfrastructureReady", "False", "ImagePullFailure",
		"failed to pull the node image", at(80))
	cpM["devmachine"].condition("ContainerProvisioned", "False", "ImagePullFailure",
		"failed to pull the node image", at(80))
	objs["cluster"].status("controlPlane", map[string]any{"desiredReplicas": int64(1), "replicas": int64(1), "readyReplicas": int64(0)})
	snap(s, &tl, 100)
	snap(s, &tl, 300)

	return tl
}
