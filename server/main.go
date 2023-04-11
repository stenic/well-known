package main

import (
	"context"
	"encoding/json"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"syscall"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/util/homedir"
	klog "k8s.io/klog/v2"

	"github.com/bep/debounce"
	"k8s.io/client-go/tools/clientcmd"
)

var kubeconfig string
var namespace string
var cmName string
var id string
var leaseLockName string

func main() {
	if home := homedir.HomeDir(); home != "" {
		flag.StringVar(&kubeconfig, "kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		flag.StringVar(&kubeconfig, "kubeconfig", "", "absolute path to the kubeconfig file")
	}
	flag.StringVar(&namespace, "namespace", "default", "namespace")
	flag.StringVar(&cmName, "configmap", "well-known-generated", "")
	flag.StringVar(&id, "id", os.Getenv("POD_NAME"), "the holder identity name")
	flag.StringVar(&leaseLockName, "lease-lock-name", "well-known", "the lease lock resource name")

	flag.Parse()

	// creates the in-cluster config
	config, err := rest.InClusterConfig()
	if err != nil {
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			panic(err.Error())
		}
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		klog.Fatal(err)
	}

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

	// we use the Lease lock type since edits to Leases are less common
	// and fewer objects in the cluster watch "all Leases".
	lock := &resourcelock.LeaseLock{
		LeaseMeta: metav1.ObjectMeta{
			Name:      leaseLockName,
			Namespace: namespace,
		},
		Client: clientset.CoordinationV1(),
		LockConfig: resourcelock.ResourceLockConfig{
			Identity: id,
		},
	}

	go func() {
		http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		klog.Info("Running /healthz endpoint on :8081")
		if err := http.ListenAndServe(":8081", nil); err != nil {
			klog.Error(err)
			os.Exit(1)
		}
	}()

	// start the leader election code loop
	leaderelection.RunOrDie(ctx, leaderelection.LeaderElectionConfig{
		Lock:            lock,
		ReleaseOnCancel: true,
		LeaseDuration:   60 * time.Second,
		RenewDeadline:   15 * time.Second,
		RetryPeriod:     5 * time.Second,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				loop(ctx, clientset)
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

func loop(ctx context.Context, clientset *kubernetes.Clientset) {
	watch, err := clientset.
		CoreV1().
		Services(namespace).
		Watch(ctx, metav1.ListOptions{})
	if err != nil {
		klog.Error(err)
		os.Exit(1)
	}

	debounced := debounce.New(500 * time.Millisecond)

	for event := range watch.ResultChan() {
		svc, ok := event.Object.(*v1.Service)
		if !ok {
			continue
		}
		klog.Infof("Change detected on %s", svc.GetName())

		debounced(func() {
			reg, err := discoverData(clientset, namespace)
			if err != nil {
				klog.Error(err)
				return
			}

			klog.Info("Writing configmap")
			if err := updateConfigMap(ctx, clientset, reg); err != nil {
				klog.Error(err)
			}
		})

	}
}

func discoverData(clientset *kubernetes.Clientset, ns string) (wkRegistry, error) {
	reg := make(wkRegistry, 0)

	svcs, err := clientset.
		CoreV1().
		Services(namespace).
		List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return reg, err
	}

	for _, svc := range svcs.Items {
		for name, value := range svc.ObjectMeta.Annotations {
			name = resolveName(name)
			if name == "" {
				continue
			}

			if _, ok := reg[name]; !ok {
				reg[name] = make(wkData, 0)
			}

			var d map[string]interface{}
			err := json.Unmarshal([]byte(value), &d)
			if err != nil {
				klog.Error(err)
			}

			reg[name].append(d)
		}
	}
	return reg, nil
}

func resolveName(name string) string {
	r := regexp.MustCompile(`^well-known.stenic.io/(.+)$`)
	if !r.MatchString(name) {
		return ""
	}
	m := r.FindStringSubmatch(name)
	if len(m) != 2 {
		return ""
	}
	return m[1]
}

func updateConfigMap(ctx context.Context, client kubernetes.Interface, reg wkRegistry) error {
	cm := &v1.ConfigMap{Data: reg.encode()}
	cm.Namespace = namespace
	cm.Name = cmName

	_, err := client.
		CoreV1().
		ConfigMaps(namespace).
		Update(ctx, cm, metav1.UpdateOptions{})
	if errors.IsNotFound(err) {
		_, err = client.
			CoreV1().
			ConfigMaps(namespace).
			Create(ctx, cm, metav1.CreateOptions{})
		if err == nil {
			klog.Infof("Created ConfigMap %s/%s\n", cm.GetNamespace(), cm.GetName())
		}
		return err
	} else if err != nil {
		klog.Error(err)
		return err
	}

	klog.Infof("Updated ConfigMap %s/%s\n", cm.GetNamespace(), cm.GetName())
	return nil
}
