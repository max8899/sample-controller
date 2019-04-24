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
	"errors"
	"fmt"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/sample-controller/util"
	"k8s.io/sample-controller/vm"
	"sync"
	"time"

	"github.com/golang/glog"
	corev1 "k8s.io/api/core/v1"
	apiError "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	samplev1alpha1 "k8s.io/sample-controller/pkg/apis/samplecontroller/v1alpha1"
	clientset "k8s.io/sample-controller/pkg/client/clientset/versioned"
	samplescheme "k8s.io/sample-controller/pkg/client/clientset/versioned/scheme"
	informers "k8s.io/sample-controller/pkg/client/informers/externalversions/samplecontroller/v1alpha1"
	listers "k8s.io/sample-controller/pkg/client/listers/samplecontroller/v1alpha1"
)

// Controller is the controller implementation for Foo resources
type VMController struct {
	// kubeclientset is a standard kubernetes clientset
	kubeclientset kubernetes.Interface
	// sampleclientset is a clientset for our own API group
	sampleclientset clientset.Interface
	vmLister        listers.VMLister
	vmSynced        cache.InformerSynced
	vmManager       vm.Interface
	// workqueue is a rate limited work queue. This is used to queue work to be
	// processed instead of performing it as soon as a change happens. This
	// means we can ensure we only process a fixed amount of resources at a
	// time, and makes it easy to ensure we are never processing the same item
	// simultaneously in two different workers.
	workqueue workqueue.RateLimitingInterface
	// recorder is an event recorder for recording Event resources to the
	// Kubernetes API.
	recorder record.EventRecorder
}

// NewController returns a new sample controller
func NewVMController(
	kubeclientset kubernetes.Interface,
	sampleclientset clientset.Interface,
	vmInformer informers.VMInformer) *VMController {

	// Create event broadcaster
	// Add sample-controller types to the default Kubernetes Scheme so Events can be
	// logged for sample-controller types.
	utilruntime.Must(samplescheme.AddToScheme(scheme.Scheme))
	glog.V(4).Info("Creating event broadcaster")
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: kubeclientset.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: controllerAgentName})

	controller := &VMController{
		kubeclientset:   kubeclientset,
		sampleclientset: sampleclientset,
		vmLister:        vmInformer.Lister(),
		vmSynced:        vmInformer.Informer().HasSynced,
		vmManager:       vm.NewVMManager(),
		workqueue:       workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "VM"),
		recorder:        recorder,
	}

	glog.Info("Setting up event handlers")
	// Set up an event handler for when Foo resources change
	vmInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueVM,
		UpdateFunc: func(old, new interface{}) {
			controller.enqueueVM(new)
		},
		DeleteFunc: controller.enqueueVM,
	})

	return controller
}

// Run will set up the event handlers for types we are interested in, as well
// as syncing informer caches and starting workers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// workers to finish processing their current work items.
func (c *VMController) Run(threadiness int, stopCh <-chan struct{}) error {
	defer runtime.HandleCrash()
	defer c.workqueue.ShutDown()

	// Start the informer factories to begin populating the informer caches
	glog.Info("Starting VM controller")

	// Wait for the caches to be synced before starting workers
	glog.Info("Waiting for informer caches to sync")
	if ok := cache.WaitForCacheSync(stopCh, c.vmSynced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	glog.Info("Starting workers")
	// Launch two workers to process VM resources
	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	go wait.Until(c.syncVMStatus, time.Second, stopCh)

	glog.Info("Started workers")
	<-stopCh
	glog.Info("Shutting down workers")

	return nil
}

// runWorker is a long-running function that will continually call the
// processNextWorkItem function in order to read and process a message on the
// workqueue.
func (c *VMController) runWorker() {
	for c.processNextWorkItem() {
	}
}

func (c *VMController) syncVMStatus() {

	labels := labels.Everything()

	vmList, err := c.vmLister.List(labels)

	if err != nil {
		glog.V(3).Infof(fmt.Sprintf( "Error listing while syncing vm status: err=%+v", err))
		return
	}

	if len(vmList) == 0 {
		return
	}

	wg := &sync.WaitGroup{}

	for _, vmObj := range vmList {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.updateVMStatus(vmObj, nil)
		}()
	}
	wg.Wait()
}

// processNextWorkItem will read a single work item off the workqueue and
// attempt to process it, by calling the syncHandler.
func (c *VMController) processNextWorkItem() bool {
	obj, shutdown := c.workqueue.Get()

	if shutdown {
		return false
	}

	// We wrap this block in a func so we can defer c.workqueue.Done.
	err := func(obj interface{}) error {
		// We call Done here so the workqueue knows we have finished
		// processing this item. We also must remember to call Forget if we
		// do not want this work item being re-queued. For example, we do
		// not call Forget if a transient error occurs, instead the item is
		// put back on the workqueue and attempted again after a back-off
		// period.
		defer c.workqueue.Done(obj)
		var key string
		var ok bool
		// We expect strings to come off the workqueue. These are of the
		// form namespace/name. We do this as the delayed nature of the
		// workqueue means the items in the informer cache may actually be
		// more up to date that when the item was initially put onto the
		// workqueue.
		if key, ok = obj.(string); !ok {
			// As the item in the workqueue is actually invalid, we call
			// Forget here else we'd go into a loop of attempting to
			// process a work item that is invalid.
			c.workqueue.Forget(obj)
			runtime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		// Run the syncHandler, passing it the namespace/name string of the
		// VM resource to be synced.
		if err := c.syncHandler(key); err != nil {
			return fmt.Errorf("error syncing '%s': %s", key, err.Error())
		}
		// Finally, if no error occurs we Forget this item so it does not
		// get queued again until another change happens.
		c.workqueue.Forget(obj)
		glog.Infof("Successfully synced '%s'", key)
		return nil
	}(obj)

	if err != nil {
		runtime.HandleError(err)
		return true
	}

	return true
}

// syncHandler compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the VM resource
// with the current status of the resource.
func (c *VMController) syncHandler(key string) error {
	// Convert the namespace/name string into a distinct namespace and name
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		runtime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	// Get the VM resource with this namespace/name
	vmObj, err := c.vmLister.VMs(namespace).Get(name)
	if err != nil {
		// The VM resource may no longer exist, in which case we stop
		// processing.
		if apiError.IsNotFound(err) {
			runtime.HandleError(fmt.Errorf("vm '%s' in work queue no longer exists", key))
			return nil
		}

		return err
	}

	// Add finalizers if not added
	if !util.ContainsString(vmObj.ObjectMeta.Finalizers, util.VMProtectionFinalizer, nil) {
		if err = c.addFinalizer(vmObj); err != nil {
			return err
		}
	}

	// vm pre-delete
	if vmObj.GetDeletionTimestamp() != nil {
		if err = c.deleteVM(vmObj); err != nil {
			return err
		}
		return c.removeFinalizer(vmObj)
	}

	var vmInstance *vm.VM

	// if status.uuid is empty, vm might not created,  try to create it.
	if types.UID("") == vmObj.Status.VMID {
		if vmInstance, err = c.createVM(vmObj); err != nil {
			return  err
		}
	}

	// Update the status block of the VM resource to reflect the
	// current state of the world
	if err = c.updateVMStatus(vmObj, vmInstance); err != nil {
		return err
	}

	c.recorder.Event(vmObj, corev1.EventTypeNormal, SuccessSynced, MessageResourceSynced)
	return nil
}


func (c *VMController) getVMFromList (name string) (*vm.VM, error) {
	vmInstanceList, err := c.vmManager.List()
	if err != nil {
		return nil, err
	}
	for _, instance := range vmInstanceList {
		if name == instance.Name {
			return &instance, nil
		}
	}
	return nil, errors.New(fmt.Sprintf("vm %s not found in list", name))
}

func (c *VMController) createVM(vmObj *samplev1alpha1.VM)(*vm.VM, error) {
	var err error
	var vmInstance *vm.VM

	if vmObj == nil {
		return nil, errors.New("skip nil obj")
	}

	vmInstance, err = c.vmManager.Create(vmObj.Name)
	if err != nil {
		if apiError.IsAlreadyExists(err) {
			return  c.getVMFromList(vmObj.Name)
		}
		return nil, err
	}

	return vmInstance, nil
}

func (c *VMController) deleteVM(vmObj *samplev1alpha1.VM) error {
	var err error
	if vmObj == nil {
		return  errors.New("skip nil pointer")
	}
	vmInstance, err := c.getVMFromList(vmObj.Name)

	if err != nil {
		return err
	}
	err = c.vmManager.Delete(vmInstance.ID)

	return nil
}

func (c *VMController) updateVMStatus(vmObj *samplev1alpha1.VM, vmInstance *vm.VM) error {

	vmID := types.UID("")

	if vmObj == nil {
		return errors.New("unexcepted parameters, nil pointer received.")
	}

	if vmObj.Status.VMID == "" {
		vmID = vmObj.Status.VMID
	}else if nil != vmInstance {
		if vmInstance.ID == "" {
			return errors.New("no vm id found")
		} else {
			vmID = vmInstance.ID
		}
	}
	var err error
	var vmInstanceStatus *vm.VMStatus

	if vmInstanceStatus, err = c.vmManager.GetStatus(vmID); err != nil {
		return err
	}

	// NEVER modify objects from the store. It's a read-only, local cache.
	// You can use DeepCopy() to make a deep copy of original object and modify this copy
	// Or create a copy manually for better performance
	vmCopy := vmObj.DeepCopy()
	vmCopy.Status.VMID = vmInstanceStatus.VMID
	vmCopy.Status.CPUUtilization = vmInstanceStatus.CPUUtilization
	// If the CustomResourceSubresources feature gate is not enabled,
	// we must use Update instead of UpdateStatus to update the Status block of the VM resource.
	// UpdateStatus will not allow changes to the Spec of the resource,
	// which is ideal for ensuring nothing other than resource status has been updated.
	_, err = c.sampleclientset.SamplecontrollerV1alpha1().VMs(vmObj.Namespace).Update(vmCopy)
	return err
}

// enqueueVM takes a VM resource and converts it into a namespace/name
// string which is then put onto the work queue. This method should *not* be
// passed resources of any type other than VM.
func (c *VMController) enqueueVM(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		runtime.HandleError(err)
		return
	}
	c.workqueue.AddRateLimited(key)
}

func (c *VMController) removeFinalizer(vmObj *samplev1alpha1.VM) error{

	if vmObj == nil {
		return errors.New("Skip nil obj")
	}
	vmCopy := vmObj.DeepCopy()
	vmCopy.ObjectMeta.Finalizers = util.RemoveString(vmCopy.ObjectMeta.Finalizers, util.VMProtectionFinalizer, nil)
	_, err := c.sampleclientset.SamplecontrollerV1alpha1().VMs(vmCopy.Namespace).Update(vmCopy)
	if err != nil {
		glog.V(3).Infof("Error removing protection finalizer from PVC %s/%s: %v", vmObj.Namespace, vmObj.Name, err)
		return err
	}
	glog.V(3).Infof("Removed protection finalizer from PVC %s/%s", vmObj.Namespace, vmObj.Name)

	return nil
}


func (c *VMController) addFinalizer(vmObj *samplev1alpha1.VM) error {

	if vmObj == nil {
		return errors.New("Skip nil obj")
	}
	vmCopy := vmObj.DeepCopy()
	vmCopy.ObjectMeta.Finalizers = append(vmCopy.ObjectMeta.Finalizers, util.VMProtectionFinalizer)
	_, err := c.sampleclientset.SamplecontrollerV1alpha1().VMs(vmCopy.Namespace).Update(vmCopy)
	if err != nil {
		glog.V(3).Infof("Error adding protection finalizer to PVC %s/%s: %v", vmObj.Namespace, vmObj.Name, err)
		return err
	}
	glog.V(3).Infof("Added protection finalizer to PVC %s/%s", vmObj.Namespace, vmObj.Name)
	return nil
}
