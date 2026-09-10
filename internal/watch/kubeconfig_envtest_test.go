//go:build envtest

package watch_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// kubeconfigFor writes a kubeconfig for the envtest API server, because the
// watcher takes a path — the same one a user passes with --kubeconfig.
func kubeconfigFor(t *testing.T, config *rest.Config) string {
	t.Helper()
	api := clientcmdapi.NewConfig()
	api.Clusters["envtest"] = &clientcmdapi.Cluster{
		Server:                   config.Host,
		CertificateAuthorityData: config.CAData,
	}
	api.AuthInfos["envtest"] = &clientcmdapi.AuthInfo{
		ClientCertificateData: config.CertData,
		ClientKeyData:         config.KeyData,
		Token:                 config.BearerToken,
	}
	api.Contexts["envtest"] = &clientcmdapi.Context{
		Cluster: "envtest", AuthInfo: "envtest", Namespace: "default",
	}
	api.CurrentContext = "envtest"

	path := filepath.Join(t.TempDir(), "kubeconfig")
	require.NoError(t, clientcmd.WriteToFile(*api, path))
	require.NoError(t, os.Chmod(path, 0o600))
	return path
}
