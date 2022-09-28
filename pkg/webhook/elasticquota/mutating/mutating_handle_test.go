/*
Copyright 2022 The Koordinator Authors.

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

package mutating

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/cache/informertest"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	"sigs.k8s.io/scheduler-plugins/pkg/apis/scheduling/v1alpha1"
	pgfake "sigs.k8s.io/scheduler-plugins/pkg/generated/clientset/versioned/fake"
	"sigs.k8s.io/scheduler-plugins/pkg/generated/informers/externalversions"
)

func makeTestHandler(t *testing.T) *ElasticQuotaMutatingHandler {
	setLoglevel("5")
	client := fake.NewClientBuilder().Build()
	sche := client.Scheme()
	sche.AddKnownTypes(schema.GroupVersion{
		Group:   "scheduling.sigs.k8s.io",
		Version: "v1alpha1",
	}, &v1alpha1.ElasticQuota{}, &v1alpha1.ElasticQuotaList{})
	decoder, _ := admission.NewDecoder(sche)
	handler := &ElasticQuotaMutatingHandler{}
	handler.InjectClient(client)
	handler.InjectDecoder(decoder)

	cacheTmp := &informertest.FakeInformers{
		InformersByGVK: map[schema.GroupVersionKind]cache.SharedIndexInformer{},
		Scheme:         sche,
	}
	pgClient := pgfake.NewSimpleClientset()
	quotaSharedInformerFactory := externalversions.NewSharedInformerFactory(pgClient, 0)
	quotaInformer := quotaSharedInformerFactory.Scheduling().V1alpha1().ElasticQuotas().Informer()
	cacheTmp.InformersByGVK[elasticquotasKind] = quotaInformer
	handler.InjectCache(cacheTmp)
	return handler
}

var elasticquotasKind = schema.GroupVersionKind{Group: "scheduling.sigs.k8s.io", Version: "v1alpha1", Kind: "ElasticQuota"}

func gvr(resource string) metav1.GroupVersionResource {
	return metav1.GroupVersionResource{
		Group:    corev1.SchemeGroupVersion.Group,
		Version:  corev1.SchemeGroupVersion.Version,
		Resource: resource,
	}
}

func TestElasticQuotaMutatingHandler_Handle(t *testing.T) {
	handler := makeTestHandler(t)
	ctx := context.Background()

	testCases := []struct {
		name    string
		request admission.Request
		allowed bool
		code    int32
	}{
		{
			name: "not a elasticQuota",
			request: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Resource:  gvr("configmaps"),
					Operation: admissionv1.Create,
				},
			},
			allowed: true,
		},
		{
			name: "elasticQuota with subresource",
			request: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Resource:    gvr("elasticquotas"),
					Operation:   admissionv1.Create,
					SubResource: "status",
				},
			},
			allowed: true,
		},
		{
			name: "elasticQuota with empty object",
			request: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Resource:  gvr("elasticquotas"),
					Operation: admissionv1.Create,
					Object:    runtime.RawExtension{},
				},
			},
			allowed: false,
			code:    http.StatusBadRequest,
		},
		{
			name: "elasticQuota update, oldObj empty",
			request: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Resource:  gvr("elasticquotas"),
					Operation: admissionv1.Update,
					Object: runtime.RawExtension{
						Raw: []byte(`{"metadata":{"name":"pod1"}}`),
					},
				},
			},
			allowed: true,
		},
		{
			name: "elasticQuota with object",
			request: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Resource:  gvr("elasticquotas"),
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw: []byte(`{"metadata":{"name":"quota1"}}`),
					},
				},
			},
			allowed: true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			response := handler.Handle(ctx, tc.request)
			if tc.allowed && !response.Allowed {
				t.Errorf("unexpeced failed to handler %#v", response)
			}
			if !tc.allowed && response.AdmissionResponse.Result.Code != tc.code {
				t.Errorf("unexpected code, got %v expected %v", response.AdmissionResponse.Result.Code, tc.code)
			}
		})
	}
}

func setLoglevel(logLevel string) {
	var level klog.Level
	if err := level.Set(logLevel); err != nil {
		fmt.Printf("failed set klog.logging.verbosity %v: %v", logLevel, err)
	}
	fmt.Printf("successfully set klog.logging.verbosity to %v", logLevel)
}