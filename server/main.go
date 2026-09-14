package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/util/homedir"
	klog "k8s.io/klog/v2"

	"k8s.io/client-go/tools/clientcmd"
)

var (
	kubeconfig    string
	namespace     string
	cmName        string
	id            string
	leaseLockName string

	serverPort string
	healthPort string
	resources  []schema.GroupVersionResource
)

const (
	defaultResources     = "gateway.networking.k8s.io/v1/gateways,gateway.networking.k8s.io/v1/httproutes"
	discoveryRetryPeriod = 5 * time.Second
)

func parseFlags() {
	klog.InitFlags(nil)
	if home := homedir.HomeDir(); home != "" {
		flag.StringVar(&kubeconfig, "kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		flag.StringVar(&kubeconfig, "kubeconfig", "", "absolute path to the kubeconfig file")
	}
	flag.StringVar(&namespace, "namespace", "default", "namespace")
	flag.StringVar(&cmName, "configmap", "well-known-generated", "")
	flag.StringVar(&id, "id", os.Getenv("POD_NAME"), "the holder identity name")
	flag.StringVar(&leaseLockName, "lease-lock-name", "well-known", "the lease lock resource name")
	flag.StringVar(&serverPort, "server-port", "8080", "server port")
	flag.StringVar(&healthPort, "health-port", "8081", "health port")
	resourcesValue := flag.String("resources", defaultResources, "comma-separated namespaced resources to discover as group/version/resource")
	flag.Parse()
	var err error
	resources, err = parseResources(*resourcesValue)
	if err != nil {
		klog.Fatal(err)
	}

	if id == "" {
		klog.Fatal("id is required")
	}
}

func parseResources(value string) ([]schema.GroupVersionResource, error) {
	if value == "" {
		return nil, nil
	}

	resources := make([]schema.GroupVersionResource, 0)
	for _, value := range strings.Split(value, ",") {
		parts := strings.Split(value, "/")
		if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
			return nil, fmt.Errorf("invalid resource %q: expected group/version/resource", value)
		}
		if problems := validation.IsDNS1123Subdomain(parts[0]); len(problems) > 0 {
			return nil, fmt.Errorf("invalid resource %q: group %s", value, strings.Join(problems, ", "))
		}
		if problems := validation.IsDNS1035Label(parts[1]); len(problems) > 0 {
			return nil, fmt.Errorf("invalid resource %q: version %s", value, strings.Join(problems, ", "))
		}
		if problems := validation.IsDNS1035Label(parts[2]); len(problems) > 0 {
			return nil, fmt.Errorf("invalid resource %q: resource %s", value, strings.Join(problems, ", "))
		}
		resources = append(resources, schema.GroupVersionResource{Group: parts[0], Version: parts[1], Resource: parts[2]})
	}
	return resources, nil
}

func getClients() (*kubernetes.Clientset, dynamic.Interface) {
	config, err := rest.InClusterConfig()
	if err != nil {
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			klog.Fatal(err)
		}
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		klog.Fatal(err)
	}

	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		klog.Fatal(err)
	}

	return clientset, dynamicClient
}

func main() {
	// Parse flags
	parseFlags()

	// use a Go context so we can tell the leaderelection code when we
	// want to step down
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// listen for interrupts or the Linux SIGTERM signal and cancel
	// our context, which the leader election code will observe and
	// step down
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-ch
		klog.Info("Received termination, signaling shutdown")
		cancel()
	}()

	// Connect to the cluster
	clientset, dynamicClient := getClients()

	wks := NewWellKnownService(clientset, dynamicClient, namespace, cmName, resources)

	// Start the server
	go func() {
		klog.Infof("Running /.well-known/{id} endpoint on :%s", serverPort)
		if err := http.ListenAndServe(":"+serverPort, GetServer(wks)); err != nil {
			klog.Error(err)
			os.Exit(1)
		}
	}()

	// Start the health server
	go func() {
		klog.Infof("Running /healthz endpoint on :%s", healthPort)
		if err := http.ListenAndServe(":"+healthPort, GetHealthServer()); err != nil {
			klog.Error(err)
			os.Exit(1)
		}
	}()

	// Start the leader election code loop
	leaderelection.RunOrDie(ctx, leaderelection.LeaderElectionConfig{
		Lock: &resourcelock.LeaseLock{
			LeaseMeta: metav1.ObjectMeta{
				Name:      leaseLockName,
				Namespace: namespace,
			},
			Client: clientset.CoordinationV1(),
			LockConfig: resourcelock.ResourceLockConfig{
				Identity: id,
			},
		},
		ReleaseOnCancel: true,
		LeaseDuration:   60 * time.Second,
		RenewDeadline:   15 * time.Second,
		RetryPeriod:     5 * time.Second,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				for {
					select {
					case <-ctx.Done():
						return
					default:
						if err := wks.DiscoveryLoop(ctx); err != nil && !errors.Is(err, context.Canceled) {
							klog.Error(err)
						}
						select {
						case <-ctx.Done():
							return
						case <-time.After(discoveryRetryPeriod):
						}
					}
				}
			},
			OnStoppedLeading: func() {
				klog.Infof("leader lost: %s", id)
				os.Exit(0)
			},
			OnNewLeader: func(identity string) {
				if identity == id {
					return
				}
				klog.Infof("new leader elected: %s", identity)
			},
		},
	})
}
