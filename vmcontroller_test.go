/*
Copyright 2017 The Kubernetes Authors.

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

package main

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kubeinformers "k8s.io/client-go/informers"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	core "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/sample-controller/util"
	"testing"

	samplecontroller "k8s.io/sample-controller/pkg/apis/samplecontroller/v1alpha1"
	"k8s.io/sample-controller/pkg/client/clientset/versioned/fake"
	informers "k8s.io/sample-controller/pkg/client/informers/externalversions"
)

type vmFixture struct {
	t *testing.T

	client     *fake.Clientset
	kubeclient *k8sfake.Clientset
	// Objects to put in the store.
	vmLister []*samplecontroller.VM
	// Actions expected to happen on the client.
	kubeactions []core.Action
	actions     []core.Action
	// Objects from here preloaded into NewSimpleFake.
	kubeobjects []runtime.Object
	objects     []runtime.Object
}

func newVMFixture(t *testing.T) *vmFixture {
	f := &vmFixture{}
	f.t = t
	f.objects = []runtime.Object{}
	f.kubeobjects = []runtime.Object{}
	return f
}

func newVM(name string) *samplecontroller.VM {
	return &samplecontroller.VM{
		TypeMeta: metav1.TypeMeta{APIVersion: samplecontroller.SchemeGroupVersion.String()},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: metav1.NamespaceDefault,
		},
		Spec: samplecontroller.VMSpec{
			VMName: name,
		},
	}
}

func (f *vmFixture) newVMController() (*VMController, informers.SharedInformerFactory, kubeinformers.SharedInformerFactory) {
	f.client = fake.NewSimpleClientset(f.objects...)
	f.kubeclient = k8sfake.NewSimpleClientset(f.kubeobjects...)

	i := informers.NewSharedInformerFactory(f.client, noResyncPeriodFunc())
	k8sI := kubeinformers.NewSharedInformerFactory(f.kubeclient, noResyncPeriodFunc())

	c := NewVMController(f.kubeclient, f.client, i.Samplecontroller().V1alpha1().VMs())

	c.vmSynced = alwaysReady
	c.recorder = &record.FakeRecorder{}

	for _, f := range f.vmLister {
		i.Samplecontroller().V1alpha1().VMs().Informer().GetIndexer().Add(f)
	}

	return c, i, k8sI
}

func (f *vmFixture) run(vmName string) {
	f.runController(vmName, true, false)
}

func (f *vmFixture) runExpectError(vmName string) {
	f.runController(vmName, true, true)
}

func (f *vmFixture) runController(vmName string, startInformers bool, expectError bool) {
	c, i, k8sI := f.newVMController()
	if startInformers {
		stopCh := make(chan struct{})
		defer close(stopCh)
		i.Start(stopCh)
		k8sI.Start(stopCh)
	}

	err := c.syncHandler(vmName)
	if !expectError && err != nil {
		f.t.Errorf("error syncing vm: %v", err)
	} else if expectError && err == nil {
		f.t.Error("expected error syncing foo, got nil")
	}

	actions := filterVMInformerActions(f.client.Actions())
	for i, action := range actions {
		if len(f.actions) < i+1 {
			f.t.Errorf("%d unexpected actions: %+v", len(actions)-len(f.actions), actions[i:])
			break
		}

		expectedAction := f.actions[i]
		checkAction(expectedAction, action, f.t)
	}

	if len(f.actions) > len(actions) {
		f.t.Errorf("%d additional expected actions:%+v", len(f.actions)-len(actions), f.actions[len(actions):])
	}

	k8sActions := filterVMInformerActions(f.kubeclient.Actions())
	for i, action := range k8sActions {
		if len(f.kubeactions) < i+1 {
			f.t.Errorf("%d unexpected actions: %+v", len(k8sActions)-len(f.kubeactions), k8sActions[i:])
			break
		}

		expectedAction := f.kubeactions[i]
		checkAction(expectedAction, action, f.t)
	}

	if len(f.kubeactions) > len(k8sActions) {
		f.t.Errorf("%d additional expected actions:%+v", len(f.kubeactions)-len(k8sActions), f.kubeactions[len(k8sActions):])
	}
}

// filterVMInformerActions filters list and watch actions for testing resources.
// Since list and watch don't change resource state we can filter it to lower
// nose level in our tests.
func filterVMInformerActions(actions []core.Action) []core.Action {
	ret := []core.Action{}
	for _, action := range actions {
		if len(action.GetNamespace()) == 0 &&
			(action.Matches("list", "vms") ||
				action.Matches("watch", "vms")) {
			continue
		}
		ret = append(ret, action)
	}

	return ret
}

func (f *vmFixture) expectCreateVMAction(d *samplecontroller.VM) {
	f.actions = append(f.actions, core.NewCreateAction(schema.GroupVersionResource{Resource: "vms", Group: "samplecontroller.k8s.io", Version: "v1alpha1"}, d.Namespace, d))
}

func (f *vmFixture) expectUpdateVMAction(d *samplecontroller.VM) {
	f.actions = append(f.actions, core.NewUpdateAction(schema.GroupVersionResource{Resource: "vms", Group: "samplecontroller.k8s.io", Version: "v1alpha1"}, d.Namespace, d))
}

func (f *vmFixture) expectUpdateVMStatusAction(vmObj *samplecontroller.VM) {
	action := core.NewUpdateAction(schema.GroupVersionResource{Resource: "vms", Group: "samplecontroller.k8s.io", Version: "v1alpha1"}, vmObj.Namespace, vmObj)

	// TODO: Until #38113 is merged, we can't use Subresource
	action.Subresource = "status"
	f.actions = append(f.actions, action)
}

func (f *vmFixture) expectDeleteVMAction(vmObj *samplecontroller.VM) {
	action := core.NewDeleteAction(schema.GroupVersionResource{Resource: "vms"}, vmObj.Namespace, vmObj.Name)
	f.actions = append(f.actions, action)
}

func getVMKey(vmObj *samplecontroller.VM, t *testing.T) string {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(vmObj)
	if err != nil {
		t.Errorf("Unexpected error getting key for foo %v: %v", vmObj.Name, err)
		return ""
	}
	return key
}

func TestCreatesVM(t *testing.T) {
	f := newVMFixture(t)
	vmTest := newVM("test")

	f.vmLister = append(f.vmLister, vmTest)
	f.objects = append(f.objects, vmTest)

	vmTest.Finalizers = append(vmTest.Finalizers, util.VMProtectionFinalizer)

	f.expectUpdateVMAction(vmTest)
	//f.expectUpdateVMStatusAction(vmTest)

	f.run(getVMKey(vmTest, t))
}
