package main

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/bep/debounce"
	"github.com/davegardnerisme/deephash"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	klog "k8s.io/klog/v2"
)

type WellKnownService struct {
	clientset *kubernetes.Clientset
	namespace string
	cmName    string

	localCache *wkRegistry
}

func NewWellKnownService(clientset *kubernetes.Clientset, namespace string, cmName string) *WellKnownService {
	return &WellKnownService{
		clientset: clientset,
		namespace: namespace,
		cmName:    cmName,
	}
}

func (s *WellKnownService) GetData(ctx context.Context) (*wkRegistry, error) {
	if s.localCache != nil {
		return s.localCache, nil
	}

	cm, err := s.clientset.CoreV1().ConfigMaps(s.namespace).Get(ctx, s.cmName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}

	reg := make(wkRegistry, 0)
	for name, data := range cm.Data {
		var d wkData
		if err := json.Unmarshal([]byte(data), &d); err != nil {
			klog.Error(err)
		}
		reg[strings.TrimSuffix(name, ".json")] = d
	}

	return &reg, nil
}

func (s *WellKnownService) UpdateConfigMap(ctx context.Context, reg wkRegistry) error {
	s.localCache = &reg

	encoded, err := reg.encode()
	if err != nil {
		return err
	}

	cm := &v1.ConfigMap{Data: encoded}
	cm.Namespace = s.namespace
	cm.Name = s.cmName

	_, err = s.clientset.
		CoreV1().
		ConfigMaps(s.namespace).
		Update(ctx, cm, metav1.UpdateOptions{})
	if errors.IsNotFound(err) {
		_, err = s.clientset.
			CoreV1().
			ConfigMaps(s.namespace).
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

func (s *WellKnownService) DiscoveryLoop(ctx context.Context) error {
	watch, err := s.clientset.
		CoreV1().
		Services(s.namespace).
		Watch(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}

	debounced := debounce.New(500 * time.Millisecond)
	hash := []byte{}

	for event := range watch.ResultChan() {
		svc, ok := event.Object.(*v1.Service)
		if !ok {
			continue
		}
		klog.V(1).Infof("Change detected on %s", svc.GetName())

		debounced(func() {
			reg, err := s.collectData(ctx)
			if err != nil {
				klog.Error(err)
				return
			}

			newHash := deephash.Hash(reg)
			if string(hash) == string(newHash) {
				klog.V(1).Info("No changes detected")
				return
			}
			hash = newHash

			klog.Info("Writing configmap")
			if err := s.UpdateConfigMap(ctx, reg); err != nil {
				klog.Error(err)
			}
		})
	}
	return nil
}

func (s *WellKnownService) collectData(ctx context.Context) (wkRegistry, error) {
	reg := make(wkRegistry, 0)

	svcs, err := s.clientset.
		CoreV1().
		Services(s.namespace).
		List(ctx, metav1.ListOptions{})
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

			var d map[string]any
			err := json.Unmarshal([]byte(value), &d)
			if err != nil {
				klog.Errorf("Failed to unmarshal annotation %s: %v", name, err)
				continue
			}

			reg[name].append(d)
		}
	}
	return reg, nil
}
