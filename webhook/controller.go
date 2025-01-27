package main

import (
	"context"
	"fmt"
	"log"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

type Controller struct {
	client   dynamic.Interface
	queue    workqueue.RateLimitingInterface
	informer cache.SharedIndexInformer
}

func NewController(client dynamic.Interface, informer cache.SharedIndexInformer) *Controller {
	c := &Controller{
		client:   client,
		queue:    workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		informer: informer,
	}

	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(obj)
			if err == nil {
				c.queue.Add(key)
			}
		},
	})

	return c
}

func createCAProvider(namespace string, client dynamic.Interface) error {
	gvr := schema.GroupVersionResource{
		Group:    "cacerts.csi.cert-manager.io",
		Version:  "v1alpha1",
		Resource: "caproviderclasses",
	}

	_, err := client.Resource(gvr).Namespace(namespace).Get(context.Background(), "ca-provider", metav1.GetOptions{})
	if err == nil {
		log.Printf("CAProviderClass already exists in namespace %s", namespace)
		return nil
	}

	if !k8serrors.IsNotFound(err) {
		return fmt.Errorf("error checking CAProviderClass: %v", err)
	}

	caProvider := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "cacerts.csi.cert-manager.io/v1alpha1",
			"kind":       "CAProviderClass",
			"metadata": map[string]interface{}{
				"name":      "ca-provider",
				"namespace": namespace,
			},
			"spec": map[string]interface{}{
				"refs": []interface{}{
					map[string]interface{}{
						"kind":      "Secret",
						"name":      "ca",
						"namespace": "spire",
						"apiGroup":  "",
					},
				},
			},
		},
	}

	_, err = client.Resource(gvr).Namespace(namespace).Create(context.Background(), caProvider, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create CAProviderClass: %v", err)
	}

	log.Printf("Successfully created CAProviderClass in namespace %s", namespace)
	return nil
}

func (c *Controller) processNextWorkItem() bool {
	obj, shutdown := c.queue.Get()
	if shutdown {
		return false
	}

	defer c.queue.Done(obj)

	namespace, ok := obj.(string)
	if !ok {
		c.queue.Forget(obj)
		log.Printf("Expected string in workqueue but got %#v", obj)
		return true
	}

	if err := createCAProvider(namespace, c.client); err != nil {
		log.Printf("Error creating CAProvider in namespace %s: %v", namespace, err)
		c.queue.AddRateLimited(obj)
		return true
	}

	c.queue.Forget(obj)
	log.Printf("Successfully processed namespace %s", namespace)
	return true
}

func (c *Controller) Run(stopCh <-chan struct{}) {
	defer c.queue.ShutDown()

	log.Printf("Starting namespace controller")
	go c.informer.Run(stopCh)

	if !cache.WaitForCacheSync(stopCh, c.informer.HasSynced) {
		log.Printf("Failed to sync cache")
		return
	}

	go c.runWorker()
	<-stopCh
	log.Printf("Shutting down namespace controller")
}

func (c *Controller) runWorker() {
	for c.processNextWorkItem() {
	}
}
