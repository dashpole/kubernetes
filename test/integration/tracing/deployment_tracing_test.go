package tracing

import (
	"context"
	"fmt"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	traceservice "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracev1 "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/grpc"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"strings"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2/ktesting"
	kubeapiservertesting "k8s.io/kubernetes/cmd/kube-apiserver/app/testing"
	tracingapi "k8s.io/component-base/tracing/api/v1"
	componentbasetracing "k8s.io/component-base/tracing"
	"k8s.io/kubernetes/pkg/controller/deployment"
	"k8s.io/kubernetes/pkg/controller/replicaset"
	"k8s.io/kubernetes/test/integration/framework"
	testutil "k8s.io/kubernetes/test/integration/util"
)

type traceServer struct {
	traceservice.UnimplementedTraceServiceServer
	mu    sync.Mutex
	spans []*tracev1.Span
}

func (t *traceServer) Export(ctx context.Context, req *traceservice.ExportTraceServiceRequest) (*traceservice.ExportTraceServiceResponse, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, resourceSpans := range req.GetResourceSpans() {
		for _, scopeSpans := range resourceSpans.GetScopeSpans() {
			for _, span := range scopeSpans.GetSpans() {
				t.spans = append(t.spans, span)
			}
		}
	}
	return &traceservice.ExportTraceServiceResponse{}, nil
}

func (t *traceServer) getSpans() []*tracev1.Span {
	t.mu.Lock()
	defer t.mu.Unlock()
	spans := make([]*tracev1.Span, len(t.spans))
	copy(spans, t.spans)
	return spans
}

func TestTracing_DeploymentLifecycle(t *testing.T) {
	// 1. Setup the dummy gRPC trace Server
	srv := grpc.NewServer()
	fakeServer := &traceServer{}
	traceservice.RegisterTraceServiceServer(srv, fakeServer)
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve(l)
	defer srv.Stop()

	// 2. Setup the tracing config file
	tracingConfigFile, err := os.CreateTemp("", "tracing-config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tracingConfigFile.Name())
	if _, err := tracingConfigFile.Write([]byte(fmt.Sprintf(`
apiVersion: apiserver.config.k8s.io/v1alpha1
kind: TracingConfiguration
endpoint: %s
samplingRatePerMillion: 1000000
`, l.Addr().String()))); err != nil {
		t.Fatal(err)
	}

	// 3. Set global TracerProvider for in-process components
	ctx := context.Background()
	ep := l.Addr().String()
	rate := int32(1000000)
	cfg := &tracingapi.TracingConfiguration{
		Endpoint: &ep,
		SamplingRatePerMillion: &rate,
	}
	tp, err := componentbasetracing.NewProvider(ctx, cfg, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(componentbasetracing.Propagators())
	defer tp.Shutdown(ctx)

	// 4. Start API Server with tracing enabled
	server := kubeapiservertesting.StartTestServerOrDie(t,
		kubeapiservertesting.NewDefaultTestServerOptions(),
		[]string{
			"--tracing-config-file=" + tracingConfigFile.Name(),
			"--enable-admission-plugins=TracingContext",
		},
		framework.SharedEtcd(),
	)
	defer server.TearDownFn()

	server.ClientConfig.Wrap(componentbasetracing.WrapperFor(tp))
	clientSet, err := clientset.NewForConfig(server.ClientConfig)
	if err != nil {
		t.Fatal(err)
	}

	// 5. Start Informers
	_, tCtx := ktesting.NewTestContext(t)
	tCtx, cancel := context.WithCancel(tCtx)
	defer cancel()

	informerFactory := informers.NewSharedInformerFactory(clientSet, 0)

	// 6. Start Deployment Controller
	dc, err := deployment.NewDeploymentController(
		tCtx,
		informerFactory.Apps().V1().Deployments(),
		informerFactory.Apps().V1().ReplicaSets(),
		informerFactory.Core().V1().Pods(),
		clientSet,
	)
	if err != nil {
		t.Fatal(err)
	}
	go dc.Run(tCtx, 5)

	// 7. Start ReplicaSet Controller
	rsc := replicaset.NewReplicaSetController(
		tCtx,
		informerFactory.Apps().V1().ReplicaSets(),
		informerFactory.Core().V1().Pods(),
		clientSet,
		500,
	)
	go rsc.Run(tCtx, 5)

	testCtx := testutil.InitTestSchedulerWithOptions(t, &testutil.TestContext{
		Ctx: tCtx,
		ClientSet: clientSet,
		KubeConfig: server.ClientConfig,
		CloseFn: func() {},
	}, 0)
	defer testCtx.CloseFn()
	testutil.SyncSchedulerInformerFactory(testCtx)
	go testCtx.Scheduler.Run(testCtx.SchedulerCtx)

	informerFactory.Start(tCtx.Done())
	informerFactory.WaitForCacheSync(tCtx.Done())

	// 9. Start Fake Kubelet
	go startFakeKubelet(tCtx, clientSet, "fake-node", otel.Tracer("k8s.io/kubernetes/pkg/kubelet"))

	// 10. Execute Test Logic
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "tracing-test"}}
	_, err = clientSet.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}

	sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "default", Namespace: "tracing-test"}}
	_, err = clientSet.CoreV1().ServiceAccounts("tracing-test").Create(ctx, sa, metav1.CreateOptions{})
	if err != nil {
		// Tolerate AlreadyExists if something else created it
		t.Logf("ServiceAccount creation returned: %v", err)
	}

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "fake-node"},
	}
	node, err = clientSet.CoreV1().Nodes().Create(ctx, node, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	node.Status = corev1.NodeStatus{
		Capacity: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("8"),
			corev1.ResourceMemory: resource.MustParse("16Gi"),
			corev1.ResourcePods:   resource.MustParse("100"),
		},
		Allocatable: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("8"),
			corev1.ResourceMemory: resource.MustParse("16Gi"),
			corev1.ResourcePods:   resource.MustParse("100"),
		},
		Conditions: []corev1.NodeCondition{
			{
				Type:   corev1.NodeReady,
				Status: corev1.ConditionTrue,
			},
		},
	}
	_, err = clientSet.CoreV1().Nodes().UpdateStatus(ctx, node, metav1.UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}

	// Remove any taints that were automatically injected
	node, err = clientSet.CoreV1().Nodes().Get(ctx, node.Name, metav1.GetOptions{})
	if err == nil && len(node.Spec.Taints) > 0 {
		node.Spec.Taints = nil
		clientSet.CoreV1().Nodes().Update(ctx, node, metav1.UpdateOptions{})
	}

	replicas := int32(1)
	deploymentObj := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-deployment",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "test"},
				},
				Spec: corev1.PodSpec{
					Tolerations: []corev1.Toleration{
						{
							Operator: corev1.TolerationOpExists,
						},
					},
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "nginx",
						},
					},
				},
			},
		},
	}

	// Send an API call with our own parent trace ctx just to ensure we know the root ID
	tracer := otel.Tracer("test")
	ctxTrace, rootSpan := tracer.Start(ctx, "CreateDeployment")
	
	_, err = clientSet.AppsV1().Deployments(ns.Name).Create(ctxTrace, deploymentObj, metav1.CreateOptions{})
	rootSpan.End()
	if err != nil {
		t.Fatal(err)
	}

	// Wait for the Deployment to be available
	err = wait.PollUntilContextTimeout(ctx, 100*time.Millisecond, 20*time.Second, true, func(ctx context.Context) (bool, error) {
		d, err := clientSet.AppsV1().Deployments(ns.Name).Get(ctx, "test-deployment", metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		return d.Status.AvailableReplicas == 1, nil
	})
	if err != nil {
		pods, _ := clientSet.CoreV1().Pods(ns.Name).List(ctx, metav1.ListOptions{})
		for _, p := range pods.Items {
			t.Logf("Pod %s phase: %s, node: %s, conditions: %v", p.Name, p.Status.Phase, p.Spec.NodeName, p.Status.Conditions)
		}
		
		n, _ := clientSet.CoreV1().Nodes().Get(ctx, "fake-node", metav1.GetOptions{})
		t.Logf("Node fake-node taints: %v", n.Spec.Taints)
		
		t.Fatal("Deployment did not become available:", err)
	}

	// Retrieve spans and verify
	expectedSpans := []string{
		"syncDeployment",
		"syncReplicaSet",
		"ScheduleOne",
		"fake-kubelet-sync",
	}

	var foundSpans map[string]*tracev1.Span
	var spans []*tracev1.Span
	err = wait.PollUntilContextTimeout(ctx, 100*time.Millisecond, 20*time.Second, true, func(ctx context.Context) (bool, error) {
		spans = fakeServer.getSpans()
		
		foundSpans = map[string]*tracev1.Span{}
		for _, span := range spans {
			foundSpans[span.Name] = span
		}

		for _, expected := range expectedSpans {
			if _, ok := foundSpans[expected]; !ok {
				return false, nil
			}
		}

		// Verify trace propagation linkage
		for _, expected := range expectedSpans {
			span := foundSpans[expected]
			if len(span.Links) == 0 && span.ParentSpanId == nil {
				// Not properly linked
				return false, nil
			}
		}

		// Print debug info for all spans to see the linkage
		for _, span := range spans {
			linkTraceIds := []string{}
			for _, l := range span.Links {
				linkTraceIds = append(linkTraceIds, fmt.Sprintf("%x", l.TraceId))
			}
			t.Logf("Span %s: TraceId=%x, ParentSpanId=%x, Links=[%s]", span.Name, span.TraceId, span.ParentSpanId, strings.Join(linkTraceIds, ", "))
		}
		
		return true, nil
	})
	
	if err != nil {
		spans := fakeServer.getSpans()
		var spanNames []string
		for _, span := range spans {
			spanNames = append(spanNames, span.Name)
		}
		var failedSpans []string
	for _, expected := range expectedSpans {
		span := foundSpans[expected]
		failedSpans = append(failedSpans, fmt.Sprintf("%s (ParentSpanId: %x, Links: %d)", expected, span.ParentSpanId, len(span.Links)))
	}
	t.Fatalf("Failed waiting for all expected spans to arrive and link. Details: %v", failedSpans)
	}
}

// Fake Kubelet implementation
func startFakeKubelet(ctx context.Context, client clientset.Interface, nodeName string, tracer trace.Tracer) {
	informerFactory := informers.NewSharedInformerFactory(client, 0)
	podInformer := informerFactory.Core().V1().Pods()
	podInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			pod := obj.(*corev1.Pod)
			if pod.Spec.NodeName == nodeName && pod.Status.Phase != corev1.PodRunning {
				syncPod(client, pod, tracer)
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			pod := newObj.(*corev1.Pod)
			if pod.Spec.NodeName == nodeName && pod.Status.Phase != corev1.PodRunning {
				syncPod(client, pod, tracer)
			}
		},
	})
	informerFactory.Start(ctx.Done())
	informerFactory.WaitForCacheSync(ctx.Done())
	<-ctx.Done()
}

func syncPod(client clientset.Interface, pod *corev1.Pod, tracer trace.Tracer) {
	ctx, span := componentbasetracing.StartReconcileSpan(
		context.Background(),
		"fake-kubelet-sync",
		pod,
		tracer,
	)
	defer span.End()

	span.AddEvent("Mounting fake volumes")

	podCopy := pod.DeepCopy()
	podCopy.Status.Phase = corev1.PodRunning
	podCopy.Status.Conditions = []corev1.PodCondition{
		{Type: corev1.PodReady, Status: corev1.ConditionTrue},
	}

	_, _ = client.CoreV1().Pods(podCopy.Namespace).UpdateStatus(ctx, podCopy, metav1.UpdateOptions{})
}
