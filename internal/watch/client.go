package watch

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"capi-distro/internal/snapshot"
)

// Client reads one cluster's objects from a management cluster and keeps them
// current. It produces exactly the envelope the fixtures hold, which is the whole
// reason fold and why can be pure.
type Client struct {
	dynamic   dynamic.Interface
	discovery discovery.DiscoveryInterface
	namespace string

	mu    sync.Mutex
	cache map[string]snapshot.Object
}

// capiGroups are the API groups a cluster's objects live in. Everything CAPI and
// its providers create is under one of these suffixes, so this is a filter on the
// discovery result rather than a hardcoded list of kinds.
var capiGroupSuffixes = []string{
	"cluster.x-k8s.io",
}

// New builds a client from a kubeconfig path, or from the ambient config when the
// path is empty.
func New(kubeconfig, namespace string) (*Client, error) {
	config, err := loadConfig(kubeconfig)
	if err != nil {
		return nil, err
	}
	// Discovery lists every kind the providers serve, including deprecated ones
	// the user never named, and the API server returns a warning for each. They
	// are about this tool's listing, not about anything the user wrote, so they
	// are dropped rather than printed over the four rows.
	config.WarningHandler = rest.NoWarnings{}
	dyn, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	disco, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return nil, err
	}
	return &Client{dynamic: dyn, discovery: disco, namespace: namespace, cache: map[string]snapshot.Object{}}, nil
}

func loadConfig(kubeconfig string) (*rest.Config, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfig != "" {
		rules.ExplicitPath = kubeconfig
	}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{}).ClientConfig()
}

// Resources returns the CAPI resources that can be listed and watched in this
// management cluster. Discovery, not a hardcoded list: a provider installed later
// shows up without a code change.
func (c *Client) Resources() ([]schema.GroupVersionResource, error) {
	// Preferred versions only. CAPI serves v1beta1 and v1beta2 of every kind at
	// this release, and listing both returns each object twice and prints a
	// deprecation warning per resource — which is what a first real run looked
	// like before this line said "Preferred".
	lists, err := c.discovery.ServerPreferredResources()
	if err != nil && len(lists) == 0 {
		return nil, fmt.Errorf("discover API resources: %w", err)
	}
	var out []schema.GroupVersionResource
	for _, list := range lists {
		gv, err := schema.ParseGroupVersion(list.GroupVersion)
		if err != nil || !isCAPIGroup(gv.Group) {
			continue
		}
		for _, resource := range list.APIResources {
			if strings.Contains(resource.Name, "/") || !verbs(resource.Verbs, "list", "watch") {
				continue
			}
			out = append(out, gv.WithResource(resource.Name))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out, nil
}

func isCAPIGroup(group string) bool {
	for _, suffix := range capiGroupSuffixes {
		if group == suffix || strings.HasSuffix(group, "."+suffix) {
			return true
		}
	}
	return false
}

func verbs(have []string, want ...string) bool {
	for _, w := range want {
		found := false
		for _, h := range have {
			if h == w {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// Snapshot lists everything once and returns the envelope for one cluster.
func (c *Client) Snapshot(ctx context.Context, name string) (snapshot.Envelope, error) {
	resources, err := c.Resources()
	if err != nil {
		return snapshot.Envelope{}, err
	}
	c.mu.Lock()
	c.cache = map[string]snapshot.Object{}
	c.mu.Unlock()

	for _, gvr := range resources {
		list, err := c.dynamic.Resource(gvr).Namespace(c.namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			// A resource the caller cannot list is not a reason to fail: the escape
			// hatch is that the user runs kubectl themselves, and a partial view
			// with a named gap beats no view.
			continue
		}
		for i := range list.Items {
			c.put(snapshot.Object(list.Items[i].Object))
		}
	}
	return c.envelope(name), nil
}

// Heartbeat is how long the watcher will stay silent when nothing is changing.
// It is under the renderer's own five-second silence budget.
const Heartbeat = 3 * time.Second

// Watch keeps the cache current and emits an envelope whenever something changes,
// debounced so the renderer is never asked to draw faster than its budget.
func (c *Client) Watch(ctx context.Context, name string, debounce time.Duration) (<-chan snapshot.Envelope, error) {
	if _, err := c.Snapshot(ctx, name); err != nil {
		return nil, err
	}
	resources, err := c.Resources()
	if err != nil {
		return nil, err
	}

	events := make(chan struct{}, 1)
	for _, gvr := range resources {
		w, err := c.dynamic.Resource(gvr).Namespace(c.namespace).Watch(ctx, metav1.ListOptions{})
		if err != nil {
			continue
		}
		go func() {
			defer w.Stop()
			for event := range w.ResultChan() {
				obj, ok := event.Object.(interface{ UnstructuredContent() map[string]any })
				if !ok {
					continue
				}
				c.put(snapshot.Object(obj.UnstructuredContent()))
				select {
				case events <- struct{}{}:
				default:
				}
			}
		}()
	}

	out := make(chan snapshot.Envelope)
	go func() {
		defer close(out)
		ticker := time.NewTicker(debounce)
		defer ticker.Stop()
		dirty := true
		last := time.Now()
		for {
			select {
			case <-ctx.Done():
				return
			case <-events:
				dirty = true
			case now := <-ticker.C:
				// A cluster that has settled stops producing events, so a consumer
				// waiting for one waits for ever: `cluster up` on an already-ready
				// cluster and `cluster fixture record` both hung on this. The
				// heartbeat also keeps the renderer's "no silent gaps" promise
				// honest against a live cluster, not only against a replay.
				if !dirty && now.Sub(last) < Heartbeat {
					continue
				}
				dirty = false
				last = now
				select {
				case out <- c.envelope(name):
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}

func (c *Client) put(o snapshot.Object) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache[o.Kind()+"/"+o.Name()] = o
}

func (c *Client) envelope(name string) snapshot.Envelope {
	c.mu.Lock()
	objects := make([]snapshot.Object, 0, len(c.cache))
	for _, o := range c.cache {
		objects = append(objects, o)
	}
	c.mu.Unlock()

	return snapshot.Envelope{
		Meta: snapshot.Meta{
			Scenario:   name,
			RecordedAt: time.Now().UTC().Format(time.RFC3339),
		},
		Objects: Reachable(objects, name),
	}
}
