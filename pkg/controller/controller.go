package controller

import (
	"fmt"
	"log"
	"time"

	"github.com/stakater/Chowkidar/pkg/actions"
	"github.com/stakater/Chowkidar/pkg/actions/slack"
	"github.com/stakater/Chowkidar/pkg/config"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

// AllNamespaces as our controller will be looking for events in all namespaces
const (
	AllNamespaces = ""
)

// Event indicate the informerEvent
type Event struct {
	key          string
	eventType    string
	namespace    string
	resourceType string
}

// Controller for checking items
type Controller struct {
	clientset        *kubernetes.Clientset
	indexer          cache.Indexer
	queue            workqueue.RateLimitingInterface
	informer         cache.Controller
	controllerConfig config.Controller
	Actions          []actions.Action
}

// Constructor for the Controller to initialize the controller
func NewController(clientset *kubernetes.Clientset, controllerConfig config.Controller) *Controller {

	controller := &Controller{
		clientset:        clientset,
		controllerConfig: controllerConfig,
	}
	queue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())
	listWatcher := cache.NewListWatchFromClient(clientset.CoreV1().RESTClient(), "pods", AllNamespaces, fields.Everything())

	indexer, informer := cache.NewIndexerInformer(listWatcher, &v1.Pod{}, 0, cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.Add,
		UpdateFunc: controller.Update,
		DeleteFunc: controller.Delete,
	}, cache.Indexers{})

	controller.indexer = indexer
	controller.informer = informer
	controller.queue = queue

	controller.Actions = populateActions(controllerConfig.Actions, controllerConfig.WatchCriterion)
	return controller

}
func populateActions(configActions []config.Action, criterion config.Criterion) []actions.Action {
	var populatedActions []actions.Action
	for _, configAction := range configActions {
		if configAction.Name == "slack" {
			s := new(slack.Slack)
			s.Init(configAction.Params, criterion)
			populatedActions = append(populatedActions, s)

		}
	}

	return populatedActions
}

//Add function to add a 'create' event to the queue
func (c *Controller) Add(obj interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	var event Event

	if err == nil {
		event.key = key
		event.eventType = "create"
		c.queue.Add(event)
	}
}

//Update function to add an 'update' event to the queue
func (c *Controller) Update(old interface{}, new interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(new)
	var event Event

	if err == nil {
		event.key = key
		var event Event
		event.eventType = "update"
		c.queue.Add(event)
	}
}

//Delete function to add a 'delete' event to the queue
func (c *Controller) Delete(obj interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	var event Event

	if err == nil {
		event.key = key
		event.eventType = "delete"
		c.queue.Add(event)
	}
}

//Run function for controller which handles the queue
func (c *Controller) Run(threadiness int, stopCh chan struct{}) {
	defer runtime.HandleCrash()

	// Let the workers stop when we are done
	defer c.queue.ShutDown()

	go c.informer.Run(stopCh)

	// Wait for all involved caches to be synced, before processing items from the queue is started
	if !cache.WaitForCacheSync(stopCh, c.informer.HasSynced) {
		runtime.HandleError(fmt.Errorf("Timed out waiting for caches to sync"))
		return
	}

	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	<-stopCh
}

func (c *Controller) runWorker() {
	for c.processNextItem() {
	}
}

func (c *Controller) processNextItem() bool {
	// Wait until there is a new item in the working queue
	event, quit := c.queue.Get()
	if quit {
		return false
	}
	// Tell the queue that we are done with processing this key. This unblocks the key for other workers
	// This allows safe parallel processing because two ingresses with the same key are never processed in
	// parallel.
	defer c.queue.Done(event)

	// Invoke the method containing the business logic
	err := c.takeAction(event.(Event))
	// Handle the error if something went wrong during the execution of the business logic
	c.handleErr(err, event)
	return true
}

func (c *Controller) takeAction(event Event) error {

	obj, _, err := c.indexer.GetByKey(event.key)
	if err != nil {
		log.Printf("Fetching object with key %s from store failed with %v", event.key, err)
	}

	// process events based on its type

	for _, action := range c.Actions {
		switch event.eventType {
		case "create":
			action.ObjectCreated(obj)
			// fmt.Printf("%v ", obj.(*v1.Pod).Spec.Containers[0].Resources)

		case "update":
			//TODO: Figure how to pass old and new object
			action.ObjectUpdated(obj, nil)

		case "delete":
			action.ObjectDeleted(obj)

		}
	}

	return nil
}

// handleErr checks if an error happened and makes sure we will retry later.
func (c *Controller) handleErr(err error, key interface{}) {
	if err == nil {
		// Forget about the #AddRateLimited history of the key on every successful synchronization.
		// This ensures that future processing of updates for this key is not delayed because of
		// an outdated error history.
		c.queue.Forget(key)
		return
	}

	// This controller retries 5 times if something goes wrong. After that, it stops trying.
	if c.queue.NumRequeues(key) < 5 {
		log.Printf("Error syncing ingress %v: %v", key, err)

		// Re-enqueue the key rate limited. Based on the rate limiter on the
		// queue and the re-enqueue history, the key will be processed later again.
		c.queue.AddRateLimited(key)
		return
	}

	c.queue.Forget(key)
	// Report to an external entity that, even after several retries, we could not successfully process this key
	runtime.HandleError(err)
	log.Printf("Dropping ingress %q out of the queue: %v", key, err)
}
