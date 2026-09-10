package main

import (
	"context"
	"time"

	"capi-distro/internal/msg"
	"capi-distro/internal/snapshot"
	"capi-distro/internal/watch"
)

// liveSource is the other half of source: the same envelope, read from a real
// management cluster.
type liveSource struct {
	client *watch.Client
	name   string
	once   bool
}

// snapshotDebounce caps how often the watcher rebuilds an envelope. The renderer
// has its own budget on top; this one keeps the API server work down.
const snapshotDebounce = 500 * time.Millisecond

func newLiveSource(g *globals, name string) (*liveSource, error) {
	if name == "" {
		return nil, msg.New(msg.ClusterNotFound, msg.Vars{Object: "(no name given)", Namespace: g.namespace})
	}
	client, err := watch.New(g.kubeconfig, g.namespace)
	if err != nil {
		return nil, msg.Wrap(msg.NoRuntime, msg.Vars{}, err)
	}
	return &liveSource{client: client, name: name}, nil
}

func (l *liveSource) Snapshots(ctx context.Context) (<-chan snapshot.Envelope, error) {
	if l.once {
		env, err := l.client.Snapshot(ctx, l.name)
		if err != nil {
			return nil, err
		}
		if _, ok := env.Cluster(); !ok {
			return nil, msg.New(msg.ClusterNotFound, msg.Vars{Object: l.name, Namespace: "the namespace"})
		}
		out := make(chan snapshot.Envelope, 1)
		out <- env
		close(out)
		return out, nil
	}
	return l.client.Watch(ctx, l.name, snapshotDebounce)
}

func (l *liveSource) Close() error { return nil }
