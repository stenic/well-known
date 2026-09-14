package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestParseResources(t *testing.T) {
	resources, err := parseResources("example.com/v1/widgets,example.com/v1beta1/gadgets")
	require.NoError(t, err)
	assert.Equal(t, []schema.GroupVersionResource{
		{Group: "example.com", Version: "v1", Resource: "widgets"},
		{Group: "example.com", Version: "v1beta1", Resource: "gadgets"},
	}, resources)

	for _, value := range []string{
		"example.com/widgets",
		"Example.com/v1/widgets",
		"example.com/V1/widgets",
		"example.com/v1/Widgets",
	} {
		_, err = parseResources(value)
		assert.Error(t, err, value)
	}
}

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

func TestDiscoveryLoop_CollectsConfiguredResources(t *testing.T) {
	configuredResources := []schema.GroupVersionResource{
		{Group: "example.com", Version: "v1", Resource: "widgets"},
		{Group: "example.com", Version: "v1beta1", Resource: "gadgets"},
	}
	widget := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "example.com/v1",
		"kind":       "Widget",
		"metadata": map[string]any{
			"name":      "widget-a",
			"namespace": "default",
			"annotations": map[string]any{
				"well-known.stenic.io/config": `{"from_widget":"true"}`,
			},
		},
	}}
	gadget := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "example.com/v1beta1",
		"kind":       "Gadget",
		"metadata": map[string]any{
			"name":      "gadget-a",
			"namespace": "default",
			"annotations": map[string]any{
				"well-known.stenic.io/config": `{"from_gadget":"true"}`,
			},
		},
	}}

	dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{
			configuredResources[0]: "WidgetList",
			configuredResources[1]: "GadgetList",
		},
	)
	svc := &WellKnownService{
		clientset:     fake.NewSimpleClientset(),
		dynamicClient: dynamicClient,
		namespace:     "default",
		cmName:        "test-cm",
		resources:     configuredResources,
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- svc.DiscoveryLoop(ctx) }()

	require.Eventually(t, func() bool {
		reg, err := svc.GetData(ctx)
		return err == nil && reg != nil
	}, 2*time.Second, 20*time.Millisecond)
	_, err := dynamicClient.Resource(configuredResources[0]).Namespace("default").Create(ctx, widget, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = dynamicClient.Resource(configuredResources[1]).Namespace("default").Create(ctx, gadget, metav1.CreateOptions{})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		reg, err := svc.GetData(ctx)
		return err == nil && reg != nil && (*reg)["config"]["from_widget"] == "true" && (*reg)["config"]["from_gadget"] == "true"
	}, 2*time.Second, 20*time.Millisecond)
	cancel()
	require.ErrorIs(t, <-errCh, context.Canceled)
}

func TestDiscoveryLoop_IgnoresMissingConfiguredResource(t *testing.T) {
	resource := schema.GroupVersionResource{Group: "example.com", Version: "v1", Resource: "widgets"}
	dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{resource: "WidgetList"},
	)
	notFound := apierrors.NewNotFound(resource.GroupResource(), "")
	dynamicClient.PrependWatchReactor(resource.Resource, func(k8stesting.Action) (bool, watch.Interface, error) {
		return true, nil, notFound
	})
	dynamicClient.PrependReactor("list", resource.Resource, func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, notFound
	})

	clientset := fake.NewSimpleClientset(&v1.Service{ObjectMeta: metav1.ObjectMeta{
		Name:      "svc-a",
		Namespace: "default",
		Annotations: map[string]string{
			"well-known.stenic.io/config": `{"from_service":"true"}`,
		},
	}})
	svc := &WellKnownService{
		clientset:     clientset,
		dynamicClient: dynamicClient,
		namespace:     "default",
		cmName:        "test-cm",
		resources:     []schema.GroupVersionResource{resource},
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- svc.DiscoveryLoop(ctx) }()

	require.Eventually(t, func() bool {
		reg, err := svc.GetData(ctx)
		return err == nil && reg != nil && (*reg)["config"]["from_service"] == "true"
	}, 2*time.Second, 20*time.Millisecond)
	cancel()
	require.ErrorIs(t, <-errCh, context.Canceled)
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

	reg, err := svc.GetData(ctx)
	require.NoError(t, err)
	require.NotNil(t, reg)
	assert.Equal(t, "from-service", (*reg)["test"]["key"])

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

	reg, err := svc.GetData(ctx)
	require.NoError(t, err)
	require.NotNil(t, reg)
	assert.Equal(t, "from-ingress", (*reg)["test"]["key"])

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

	reg, err := svc.GetData(ctx)
	require.NoError(t, err)
	require.NotNil(t, reg)
	assert.Len(t, *reg, 2)
	assert.Equal(t, "service", (*reg)["svc-config"]["origin"])
	assert.Equal(t, "ingress", (*reg)["ing-config"]["origin"])

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
