package fold

// Condition types read from the pinned API. Every name here appears in
// docs/api-snapshot.json; TestFold_OnlyUsesSnapshotConditions fails if one does
// not, which is what stops a condition name being written from memory.
const (
	// Cluster, cluster.x-k8s.io/v1beta2 (core/v1beta2/cluster_types.go).
	clusterInfrastructureReady   = "InfrastructureReady"
	clusterControlPlaneAvailable = "ControlPlaneAvailable"
	clusterControlPlaneInit      = "ControlPlaneInitialized"
	clusterWorkersAvailable      = "WorkersAvailable"
	clusterTopologyReconciled    = "TopologyReconciled"

	// Shared, set by most kinds (core/v1beta2/condition_consts.go).
	available          = "Available"
	ready              = "Ready"
	machinesReady      = "MachinesReady"
	resourcesApplied   = "ResourcesApplied"
	nodeHealthy        = "NodeHealthy"
	bootstrapConfigOK  = "BootstrapConfigReady"
	infrastructureOK   = "InfrastructureReady"
	etcdClusterHealthy = "EtcdClusterHealthy"
	etcdMemberHealthy  = "EtcdMemberHealthy"

	// CAPD Dev* kinds, in-memory backend
	// (test/infrastructure/docker/api/v1beta2/devmachine_types.go).
	etcdProvisioned      = "EtcdProvisioned"
	apiServerProvisioned = "APIServerProvisioned"
	nodeProvisioned      = "NodeProvisioned"
	vmProvisioned        = "VMProvisioned"
	// CAPD Dev* kinds, docker backend.
	containerProvisioned = "ContainerProvisioned"
	bootstrapCompleted   = "BootstrapCompleted"
)

// hostedControlPlaneKinds report pod readiness rather than node counts: there are
// no control-plane machines at all, the control plane runs as pods in the
// management cluster.
var hostedControlPlaneKinds = map[string]bool{
	"K0smotronControlPlane": true,
	"KamajiControlPlane":    true,
}
