package fixture

// The in-memory backend fakes VM, etcd, API server and node provisioning inside
// the management cluster's API server, with a configurable startup duration per
// component (docs/api-snapshot.md, Notes). That is what makes a stall inducible
// without Docker: set one component's duration past stallAfter and nothing else
// changes. These are the primary fixtures for `why` ranking and ETA math.

func hostedDockerHappy() Timeline {
	const note = "stands in for placement: hosted, a k0smotron control plane runs as pods in the " +
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
	cpM := s.controlPlaneMachine(at(50))
	cpM["machine"].condition("InfrastructureReady", "False", "ImagePullFailure",
		"failed to pull the node image", at(80))
	cpM["devmachine"].condition("ContainerProvisioned", "False", "ImagePullFailure",
		"failed to pull the node image", at(80))
	objs["cluster"].status("controlPlane", map[string]any{"desiredReplicas": int64(1), "replicas": int64(1), "readyReplicas": int64(0)})
	snap(s, &tl, 100)
	snap(s, &tl, 300)

	return tl
}
