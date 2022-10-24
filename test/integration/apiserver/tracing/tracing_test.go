/*
Copyright 2021 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package tracing

import (
	"context"
	"os"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	genericfeatures "k8s.io/apiserver/pkg/features"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	client "k8s.io/client-go/kubernetes"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
	kubeapiservertesting "k8s.io/kubernetes/cmd/kube-apiserver/app/testing"
	"k8s.io/kubernetes/test/integration/framework"
)

func TestAPIServerTracing(t *testing.T) {
	defer featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, genericfeatures.APIServerTracing, true)()
	// Write the configuration for tracing to a file
	tracingConfigFile, err := os.CreateTemp("", "tracing-config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tracingConfigFile.Name())

	if err := os.WriteFile(tracingConfigFile.Name(), []byte(`
apiVersion: apiserver.config.k8s.io/v1alpha1
kind: TracingConfiguration
samplingRatePerMillion: 1000000
endpoint: 0.0.0.0:4317`), os.FileMode(0755)); err != nil {
		t.Fatal(err)
	}

	// Start the API Server with our tracing configuration
	testServer := kubeapiservertesting.StartTestServerOrDie(t,
		kubeapiservertesting.NewDefaultTestServerOptions(),
		[]string{"--tracing-config-file=" + tracingConfigFile.Name()},
		framework.SharedEtcd(),
	)
	defer testServer.TearDownFn()
	clientSet, err := client.NewForConfig(testServer.ClientConfig)
	if err != nil {
		t.Fatal(err)
	}
	_, err = clientSet.CoreV1().Nodes().Create(context.Background(),
		&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "fake"}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Second)
	t.Fatalf("foo")
}
