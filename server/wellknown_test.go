package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestCollectData_ServicesOnly(t *testing.T) {
	clientset := fake.NewSimpleClientset(
		&v1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "svc-a",
				Namespace: "default",
				Annotations: map[string]string{
					"well-known.stenic.io/openid-configuration": `{"issuer":"https://example.com"}`,
				},
			},
		},
	)

	svc := &WellKnownService{
		clientset: clientset,
		namespace: "default",
	}

	reg, err := svc.collectData(context.Background())
	require.NoError(t, err)

	assert.Len(t, reg, 1)
	assert.Equal(t, wkData{"issuer": "https://example.com"}, reg["openid-configuration"])
}

func TestCollectData_IngressesOnly(t *testing.T) {
	clientset := fake.NewSimpleClientset(
		&networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ing-a",
				Namespace: "default",
				Annotations: map[string]string{
					"well-known.stenic.io/security.txt": `{"contact":"security@example.com"}`,
				},
			},
		},
	)

	svc := &WellKnownService{
		clientset: clientset,
		namespace: "default",
	}

	reg, err := svc.collectData(context.Background())
	require.NoError(t, err)

	assert.Len(t, reg, 1)
	assert.Equal(t, wkData{"contact": "security@example.com"}, reg["security.txt"])
}

func TestCollectData_ServicesAndIngresses(t *testing.T) {
	clientset := fake.NewSimpleClientset(
		&v1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "svc-a",
				Namespace: "default",
				Annotations: map[string]string{
					"well-known.stenic.io/openid-configuration": `{"issuer":"https://example.com"}`,
				},
			},
		},
		&networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ing-a",
				Namespace: "default",
				Annotations: map[string]string{
					"well-known.stenic.io/security.txt": `{"contact":"security@example.com"}`,
				},
			},
		},
	)

	svc := &WellKnownService{
		clientset: clientset,
		namespace: "default",
	}

	reg, err := svc.collectData(context.Background())
	require.NoError(t, err)

	assert.Len(t, reg, 2)
	assert.Equal(t, wkData{"issuer": "https://example.com"}, reg["openid-configuration"])
	assert.Equal(t, wkData{"contact": "security@example.com"}, reg["security.txt"])
}

func TestCollectData_MergesAnnotationsFromBothTypes(t *testing.T) {
	clientset := fake.NewSimpleClientset(
		&v1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "svc-a",
				Namespace: "default",
				Annotations: map[string]string{
					"well-known.stenic.io/config": `{"from_svc":"true"}`,
				},
			},
		},
		&networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ing-a",
				Namespace: "default",
				Annotations: map[string]string{
					"well-known.stenic.io/config": `{"from_ing":"true"}`,
				},
			},
		},
	)

	svc := &WellKnownService{
		clientset: clientset,
		namespace: "default",
	}

	reg, err := svc.collectData(context.Background())
	require.NoError(t, err)

	assert.Len(t, reg, 1)
	assert.Equal(t, "true", reg["config"]["from_svc"])
	assert.Equal(t, "true", reg["config"]["from_ing"])
}

func TestCollectData_IgnoresNonMatchingAnnotations(t *testing.T) {
	clientset := fake.NewSimpleClientset(
		&v1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "svc-a",
				Namespace: "default",
				Annotations: map[string]string{
					"some-other-annotation": `{"key":"value"}`,
				},
			},
		},
		&networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ing-a",
				Namespace: "default",
				Annotations: map[string]string{
					"nginx.ingress.kubernetes.io/rewrite-target": "/",
				},
			},
		},
	)

	svc := &WellKnownService{
		clientset: clientset,
		namespace: "default",
	}

	reg, err := svc.collectData(context.Background())
	require.NoError(t, err)

	assert.Len(t, reg, 0)
}

func TestCollectData_IgnoresOtherNamespaces(t *testing.T) {
	clientset := fake.NewSimpleClientset(
		&v1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "svc-a",
				Namespace: "other",
				Annotations: map[string]string{
					"well-known.stenic.io/config": `{"key":"value"}`,
				},
			},
		},
		&networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ing-a",
				Namespace: "other",
				Annotations: map[string]string{
					"well-known.stenic.io/config": `{"key":"value"}`,
				},
			},
		},
	)

	svc := &WellKnownService{
		clientset: clientset,
		namespace: "default",
	}

	reg, err := svc.collectData(context.Background())
	require.NoError(t, err)

	assert.Len(t, reg, 0)
}

func TestCollectAnnotations(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		expected    wkRegistry
	}{
		{
			name:        "matching annotation",
			annotations: map[string]string{"well-known.stenic.io/test": `{"a":"b"}`},
			expected:    wkRegistry{"test": wkData{"a": "b"}},
		},
		{
			name:        "no matching annotations",
			annotations: map[string]string{"other": "value"},
			expected:    wkRegistry{},
		},
		{
			name:        "invalid json skipped",
			annotations: map[string]string{"well-known.stenic.io/test": "not-json"},
			expected:    wkRegistry{"test": wkData{}},
		},
		{
			name:        "nil annotations",
			annotations: nil,
			expected:    wkRegistry{},
		},
		{
			name: "multiple matching annotations",
			annotations: map[string]string{
				"well-known.stenic.io/a": `{"x":"1"}`,
				"well-known.stenic.io/b": `{"y":"2"}`,
			},
			expected: wkRegistry{
				"a": wkData{"x": "1"},
				"b": wkData{"y": "2"},
			},
		},
	}

	svc := &WellKnownService{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := make(wkRegistry, 0)
			svc.collectAnnotations(reg, tt.annotations)
			assert.Equal(t, tt.expected, reg)
		})
	}
}

func TestDiscoveryLoop_ReactsToServiceEvents(t *testing.T) {
	clientset := fake.NewSimpleClientset()

	svc := &WellKnownService{
		clientset: clientset,
		namespace: "default",
		cmName:    "test-cm",
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- svc.DiscoveryLoop(ctx)
	}()

	// Give the watches time to start
	time.Sleep(100 * time.Millisecond)

	// Create a service with a well-known annotation
	_, err := clientset.CoreV1().Services("default").Create(ctx, &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-test",
			Namespace: "default",
			Annotations: map[string]string{
				"well-known.stenic.io/test": `{"key":"from-service"}`,
			},
		},
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	// Wait for debounce + processing
	time.Sleep(700 * time.Millisecond)

	assert.NotNil(t, svc.localCache)
	assert.Equal(t, "from-service", (*svc.localCache)["test"]["key"])

	cancel()
}

func TestDiscoveryLoop_ReactsToIngressEvents(t *testing.T) {
	clientset := fake.NewSimpleClientset()

	svc := &WellKnownService{
		clientset: clientset,
		namespace: "default",
		cmName:    "test-cm",
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- svc.DiscoveryLoop(ctx)
	}()

	// Give the watches time to start
	time.Sleep(100 * time.Millisecond)

	// Create an ingress with a well-known annotation
	_, err := clientset.NetworkingV1().Ingresses("default").Create(ctx, &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ing-test",
			Namespace: "default",
			Annotations: map[string]string{
				"well-known.stenic.io/test": `{"key":"from-ingress"}`,
			},
		},
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	// Wait for debounce + processing
	time.Sleep(700 * time.Millisecond)

	assert.NotNil(t, svc.localCache)
	assert.Equal(t, "from-ingress", (*svc.localCache)["test"]["key"])

	cancel()
}

func TestDiscoveryLoop_ReactsToBothEventTypes(t *testing.T) {
	clientset := fake.NewSimpleClientset()

	svc := &WellKnownService{
		clientset: clientset,
		namespace: "default",
		cmName:    "test-cm",
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- svc.DiscoveryLoop(ctx)
	}()

	// Give the watches time to start
	time.Sleep(100 * time.Millisecond)

	// Create a service
	_, err := clientset.CoreV1().Services("default").Create(ctx, &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-test",
			Namespace: "default",
			Annotations: map[string]string{
				"well-known.stenic.io/svc-config": `{"origin":"service"}`,
			},
		},
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	// Create an ingress
	_, err = clientset.NetworkingV1().Ingresses("default").Create(ctx, &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ing-test",
			Namespace: "default",
			Annotations: map[string]string{
				"well-known.stenic.io/ing-config": `{"origin":"ingress"}`,
			},
		},
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	// Wait for debounce + processing
	time.Sleep(700 * time.Millisecond)

	assert.NotNil(t, svc.localCache)
	assert.Len(t, *svc.localCache, 2)
	assert.Equal(t, "service", (*svc.localCache)["svc-config"]["origin"])
	assert.Equal(t, "ingress", (*svc.localCache)["ing-config"]["origin"])

	cancel()
}

func TestDiscoveryLoop_StopsOnClosedServiceWatch(t *testing.T) {
	clientset := fake.NewSimpleClientset()

	svc := &WellKnownService{
		clientset: clientset,
		namespace: "default",
		cmName:    "test-cm",
	}

	ctx := context.Background()

	// Get the watch reactors so we can control the watch
	svcWatcher := watch.NewFake()
	clientset.PrependWatchReactor("services", func(action k8stesting.Action) (bool, watch.Interface, error) {
		return true, svcWatcher, nil
	})

	errCh := make(chan error, 1)
	go func() {
		errCh <- svc.DiscoveryLoop(ctx)
	}()

	time.Sleep(100 * time.Millisecond)
	svcWatcher.Stop()

	select {
	case err := <-errCh:
		assert.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("DiscoveryLoop did not return after service watch closed")
	}
}
