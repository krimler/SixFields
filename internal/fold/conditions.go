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
)

// hostedControlPlaneKinds report pod readiness rather than node counts: there are
// no control-plane machines at all, the control plane runs as pods in the
// management cluster.
var hostedControlPlaneKinds = map[string]bool{
	"K0smotronControlPlane": true,
	"KamajiControlPlane":    true,
}
