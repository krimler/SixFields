package fixture

// Each function below is one scenario's timeline. They read top to bottom as the
// run happened: build the object graph, then mutate and snapshot at each instant a
// watcher would have seen a change.

func stdDockerHappy() Timeline {
	const note = "stands in for a recorded `make dev-up` run of the std class on CAPD: " +
		"infrastructure, then a single control-plane machine, then two workers"
	s, objs := build(opts{name: "std-docker-happy", note: note, cluster: "dev-1",
		placement: "self", backend: "docker", cpReplicas: 1, workerCount: 2})
	tl := Timeline{Name: s.name, Note: note}

	snap(s, &tl, 0)

	cpM := s.controlPlaneMachine("dev-1", "KubeadmControlPlane", "dev-1-cp", "dev-1-cp-abcde", at(45))
	objs["cluster"].status("controlPlane", map[string]any{"desiredReplicas": int64(1), "replicas": int64(1), "readyReplicas": int64(0)})
	snap(s, &tl, 60)

	infraReady(objs, at(90))
	snap(s, &tl, 120)

	machineReady(cpM, at(150))
	controlPlaneUp(objs, 1, at(165))
	w0 := s.workerMachine("dev-1", "dev-1-md-0", fmtName("dev-1", 0), at(170))
	w1 := s.workerMachine("dev-1", "dev-1-md-0", fmtName("dev-1", 1), at(170))
	objs["md"].status("replicas", int64(2))
	snap(s, &tl, 180)

	machineReady(w0, at(240))
	machineReady(w1, at(245))
	workersUp(objs, 2, at(250))
	snap(s, &tl, 260)

	return tl
}

// stallBadVersion is PLAN.md's first stall scenario: a Kubernetes version the
// provider has no image for. The control plane never initializes and nothing
// transitions after the failure.
func stallBadVersion() Timeline {
	const note = "hand-built stall: spec.topology.version names a version with no node image, " +
		"so the control-plane machine never provisions and nothing transitions after t+90"
	s, objs := build(opts{name: "stall-bad-version", note: note, cluster: "dev-1",
		placement: "self", backend: "docker", cpReplicas: 1, workerCount: 2})
	tl := Timeline{Name: s.name, Note: note}

	objs["cluster"].spec("topology", map[string]any{
		"classRef": map[string]any{"name": "std"}, "version": "v1.99.0",
		"variables": []any{map[string]any{"name": "size", "value": "dev"}},
		"workers": map[string]any{"machineDeployments": []any{
			map[string]any{"name": "default", "class": "default", "replicas": int64(2)}}},
	})
	snap(s, &tl, 0)

	infraReady(objs, at(60))
	cpM := s.controlPlaneMachine("dev-1", "KubeadmControlPlane", "dev-1-cp", "dev-1-cp-abcde", at(70))
	cpM["devmachine"].condition("ContainerProvisioned", "False", "ImagePullFailure",
		"failed to pull kindest/node:v1.99.0: manifest unknown", at(90))
	cpM["machine"].condition("InfrastructureReady", "False", "ImagePullFailure",
		"failed to pull kindest/node:v1.99.0: manifest unknown", at(90))
	objs["cluster"].status("controlPlane", map[string]any{"desiredReplicas": int64(1), "replicas": int64(1), "readyReplicas": int64(0)})
	snap(s, &tl, 120)
	snap(s, &tl, 240)
	snap(s, &tl, 360)

	return tl
}

// stallCPKilled is the control-plane container disappearing after the cluster was
// already up: the phase goes backwards, which is a different shape from never
// having started.
func stallCPKilled() Timeline {
	const note = "hand-built stall: the control-plane machine's container is removed after the " +
		"cluster reached Ready, so a done phase goes back to running and then stalls"
	s, objs := build(opts{name: "stall-cp-killed", note: note, cluster: "dev-1",
		placement: "self", backend: "docker", cpReplicas: 1, workerCount: 2})
	tl := Timeline{Name: s.name, Note: note}

	cpM := s.controlPlaneMachine("dev-1", "KubeadmControlPlane", "dev-1-cp", "dev-1-cp-abcde", at(45))
	infraReady(objs, at(60))
	machineReady(cpM, at(120))
	controlPlaneUp(objs, 1, at(130))
	w0 := s.workerMachine("dev-1", "dev-1-md-0", fmtName("dev-1", 0), at(140))
	w1 := s.workerMachine("dev-1", "dev-1-md-0", fmtName("dev-1", 1), at(140))
	machineReady(w0, at(200))
	machineReady(w1, at(205))
	workersUp(objs, 2, at(210))
	snap(s, &tl, 220)

	cpM["devmachine"].condition("ContainerProvisioned", "False", "ContainerMissing",
		"the machine container no longer exists", at(300))
	cpM["machine"].condition("InfrastructureReady", "False", "ContainerMissing",
		"the machine container no longer exists", at(300))
	cpM["machine"].condition("NodeHealthy", "False", "NodeNotFound", "the node is gone", at(300))
	cpM["machine"].condition("Ready", "False", "NotReady", "infrastructure is not ready", at(300))
	objs["cp"].condition("Available", "False", "NotAvailable", "0 of 1 replicas are ready", at(300))
	objs["cp"].condition("EtcdClusterHealthy", "False", "EtcdMemberNotHealthy", "etcd has lost its only member", at(300))
	objs["cp"].status("readyReplicas", int64(0))
	objs["cluster"].condition("ControlPlaneAvailable", "False", "NotAvailable", "0 of 1 replicas are ready", at(300))
	objs["cluster"].status("controlPlane", map[string]any{"desiredReplicas": int64(1), "replicas": int64(1), "readyReplicas": int64(0)})
	snap(s, &tl, 320)
	snap(s, &tl, 500)

	return tl
}

// stallBadVariable is the case the admission policy is meant to prevent. It exists
// so the CLI can prove the difference: with the policy the user is stopped at
// admission, without it they wait and then read this.
func stallBadVariable() Timeline {
	const note = "hand-built stall: a variable the class does not define reaches the topology " +
		"controller, which reports ReconcileFailed on the Cluster and creates nothing"
	s, objs := build(opts{name: "stall-bad-variable", note: note, cluster: "dev-1",
		placement: "self", backend: "docker", cpReplicas: 1, workerCount: 2, noDerived: true})
	tl := Timeline{Name: s.name, Note: note}

	objs["cluster"].condition("TopologyReconciled", "False", "ReconcileFailed",
		"variable \"gpuPool\" is not defined in ClusterClass std", at(20))
	snap(s, &tl, 30)
	snap(s, &tl, 150)
	snap(s, &tl, 300)

	return tl
}

// scaleUp widens the pool mid-run: replicas 2 -> 4 after the cluster is ready.
func scaleUp() Timeline {
	const note = "hand-built: the pool is scaled 2 -> 4 after the cluster is Ready, so the " +
		"workers phase goes from done back to running and then done again"
	s, objs := build(opts{name: "scale-up", note: note, cluster: "dev-1",
		placement: "self", backend: "docker", cpReplicas: 1, workerCount: 2})
	tl := Timeline{Name: s.name, Note: note}

	cpM := s.controlPlaneMachine("dev-1", "KubeadmControlPlane", "dev-1-cp", "dev-1-cp-abcde", at(45))
	infraReady(objs, at(60))
	machineReady(cpM, at(120))
	controlPlaneUp(objs, 1, at(130))
	w := make([]map[string]*object, 0, 4)
	for i := 0; i < 2; i++ {
		m := s.workerMachine("dev-1", "dev-1-md-0", fmtName("dev-1", i), at(140))
		machineReady(m, at(200+i*5))
		w = append(w, m)
	}
	workersUp(objs, 2, at(210))
	snap(s, &tl, 220)

	objs["md"].spec("replicas", int64(4))
	objs["md"].condition("Available", "False", "ScalingUp", "2 of 4 replicas are ready", at(300))
	objs["cluster"].condition("WorkersAvailable", "False", "NotAvailable", "2 of 4 replicas are ready", at(300))
	objs["cluster"].status("workers", map[string]any{"desiredReplicas": int64(4), "replicas": int64(4), "readyReplicas": int64(2)})
	for i := 2; i < 4; i++ {
		w = append(w, s.workerMachine("dev-1", "dev-1-md-0", fmtName("dev-1", i), at(300)))
	}
	snap(s, &tl, 320)

	machineReady(w[2], at(380))
	machineReady(w[3], at(385))
	workersUp(objs, 4, at(390))
	snap(s, &tl, 400)

	return tl
}
